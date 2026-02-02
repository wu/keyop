package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type PersistentQueue struct {
	name       string
	dir        string
	osProvider OsProviderApi
	logger     Logger
	mu         sync.Mutex
	cond       *sync.Cond
	pending    map[string]readerState // In-memory track of what's been dequeued but not acked
}

type readerState struct {
	FileName string `json:"file_name"`
	Offset   int64  `json:"offset"`
}

func NewPersistentQueue(name string, dir string, osProvider OsProviderApi, logger Logger) (*PersistentQueue, error) {
	if name == "" {
		return nil, fmt.Errorf("queue name cannot be empty")
	}
	if dir == "" {
		return nil, fmt.Errorf("directory path cannot be empty")
	}
	if osProvider == nil {
		return nil, fmt.Errorf("osProvider cannot be nil")
	}
	if err := osProvider.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	pq := &PersistentQueue{
		name:       name,
		dir:        dir,
		osProvider: osProvider,
		logger:     logger,
		mu:         sync.Mutex{},
		pending:    make(map[string]readerState),
	}
	pq.cond = sync.NewCond(&pq.mu)
	return pq, nil
}

func (pq *PersistentQueue) Enqueue(entry string) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	pq.logger.Debug("Enqueue called", "entry", entry)

	dateStr := time.Now().Format("20060102")
	fileName := filepath.Join(pq.dir, fmt.Sprintf("%s_queue_%s.log", pq.name, dateStr))

	f, err := pq.osProvider.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(entry + "\n"); err != nil {
		return err
	}

	pq.cond.Broadcast()
	return nil
}

func (pq *PersistentQueue) Dequeue(readerName string) (string, error) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	pq.logger.Debug("Dequeue called", "reader", readerName)

	for {
		state, err := pq.loadState(readerName)
		if err != nil {
			return "", err
		}

		if state.FileName == "" {
			// Find first queue file
			files, err := pq.listQueueFiles()
			if err != nil {
				return "", err
			}
			if len(files) > 0 {
				state.FileName = files[0]
				state.Offset = 0
			}
		}

		if state.FileName != "" {
			entry, nextOffset, err := pq.readEntry(state.FileName, state.Offset)
			if err == nil {
				pq.logger.Debug("Dequeue: read entry successfully", "reader", readerName, "entry", entry)
				pq.pending[readerName] = readerState{
					FileName: state.FileName,
					Offset:   nextOffset,
				}
				return entry, nil
			}

			if err == io.EOF {
				// Reached end of file. Check if we should move to next file.
				dateStr := time.Now().Format("20060102")
				currentFileDate := pq.extractDate(state.FileName)

				if currentFileDate != "" && currentFileDate < dateStr {
					// Find next file
					files, err := pq.listQueueFiles()
					if err != nil {
						return "", err
					}
					found := false
					for _, f := range files {
						if f > state.FileName {
							state.FileName = f
							state.Offset = 0
							found = true
							break
						}
					}
					if found {
						// Update persistent state to next file so we don't keep checking the old file
						if err := pq.saveState(readerName, state); err != nil {
							return "", err
						}
						continue // Try reading from the next file
					}
				}
			} else {
				return "", err
			}
		}

		// No more entries, block
		pq.cond.Wait()
	}
}

func (pq *PersistentQueue) Ack(readerName string) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	pq.logger.Debug("Ack called", "reader", readerName)

	state, ok := pq.pending[readerName]
	if !ok {
		// Nothing to ack, or already acked
		return nil
	}

	if err := pq.saveState(readerName, state); err != nil {
		return err
	}

	delete(pq.pending, readerName)
	return nil
}

func (pq *PersistentQueue) loadState(readerName string) (readerState, error) {
	var state readerState
	stateFile := filepath.Join(pq.dir, fmt.Sprintf("reader_state_%s_%s.json", pq.name, readerName))
	f, err := pq.osProvider.OpenFile(stateFile, os.O_RDONLY, 0644)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	defer f.Close()

	if err := json.NewDecoder(f).Decode(&state); err != nil {
		if err == io.EOF {
			return state, nil
		}
		return state, err
	}
	return state, nil
}

func (pq *PersistentQueue) saveState(readerName string, state readerState) error {
	stateFile := filepath.Join(pq.dir, fmt.Sprintf("reader_state_%s_%s.json", pq.name, readerName))
	// Use a temporary file for atomic write if possible, but here we just overwrite
	f, err := pq.osProvider.OpenFile(stateFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(state)
}

func (pq *PersistentQueue) listQueueFiles() ([]string, error) {
	entries, err := pq.osProvider.ReadDir(pq.dir)
	if err != nil {
		return nil, err
	}

	prefix := pq.name + "_queue_"
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) && strings.HasSuffix(entry.Name(), ".log") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func (pq *PersistentQueue) readEntry(fileName string, offset int64) (string, int64, error) {
	fullPath := filepath.Join(pq.dir, fileName)
	f, err := pq.osProvider.OpenFile(fullPath, os.O_RDONLY, 0644)
	if err != nil {
		return "", offset, err
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", offset, err
	}

	reader := bufio.NewReader(f)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", offset, err
	}

	newOffset := offset + int64(len(line))
	return strings.TrimSuffix(line, "\n"), newOffset, nil
}

func (pq *PersistentQueue) extractDate(fileName string) string {
	// <name>_queue_YYYYMMDD.log
	base := filepath.Base(fileName)
	prefix := pq.name + "_queue_"
	if strings.HasPrefix(base, prefix) && strings.HasSuffix(base, ".log") {
		datePart := strings.TrimPrefix(strings.TrimSuffix(base, ".log"), prefix)
		if len(datePart) == 8 {
			return datePart
		}
	}
	return ""
}
