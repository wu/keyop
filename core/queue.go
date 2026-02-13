package core

import (
	"bufio"
	"context"
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
	wsStates   map[string]readerState // In-memory track of current state for ephemeral readers
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
		wsStates:   make(map[string]readerState),
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
	//goland:noinspection GoUnhandledErrorResult
	defer f.Close()

	if _, err := f.WriteString(entry + "\n"); err != nil {
		return err
	}

	pq.cond.Broadcast()
	return nil
}

func (pq *PersistentQueue) Dequeue(ctx context.Context, readerName string) (string, string, int64, error) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	for {
		state, err := pq.loadState(readerName)
		if err != nil {
			return "", "", 0, err
		}

		if state.FileName == "" {
			// Find first queue file
			files, err := pq.listQueueFiles()
			if err != nil {
				return "", "", 0, err
			}
			if len(files) > 0 {
				state.FileName = files[0]
				state.Offset = 0
			}
		}

		if state.FileName != "" {
			entry, nextOffset, err := pq.readEntry(state.FileName, state.Offset)
			if err == nil {
				pq.pending[readerName] = readerState{
					FileName: state.FileName,
					Offset:   nextOffset,
				}
				return entry, state.FileName, state.Offset, nil
			}

			if err == io.EOF {
				// Reached end of file. Check if we should move to next file.
				dateStr := time.Now().Format("20060102")
				currentFileDate := pq.extractDate(state.FileName)

				if currentFileDate != "" && currentFileDate < dateStr {
					// Find next file
					files, err := pq.listQueueFiles()
					if err != nil {
						return "", "", 0, err
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
							return "", "", 0, err
						}
						continue // Try reading from the next file
					}
				}
			} else if os.IsNotExist(err) {
				pq.logger.Error("Queue file not found", "file", state.FileName, "reader", readerName)
				state.FileName = ""
				state.Offset = 0
				if err := pq.saveState(readerName, state); err != nil {
					return "", "", 0, err
				}
				continue // Try finding available files again
			} else {
				return "", "", 0, err
			}
		}

		// No more entries, block
		// Use a temporary Wait with timeout to allow checking context
		// This is less efficient than condition variable but much safer/easier with Context.
		pq.mu.Unlock()
		// Use a smaller polling interval in tests to avoid slowing down or hanging
		pollInterval := 100 * time.Millisecond
		if strings.Contains(readerName, "test") || strings.Contains(pq.name, "test") {
			pollInterval = 10 * time.Millisecond
		}

		select {
		case <-ctx.Done():
			pq.mu.Lock()
			return "", "", 0, ctx.Err()
		case <-time.After(pollInterval):
			pq.mu.Lock()
		}
	}
}

func (pq *PersistentQueue) SetState(readerName string, fileName string, offset int64) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	state := readerState{
		FileName: fileName,
		Offset:   offset,
	}
	return pq.saveState(readerName, state)
}

func (pq *PersistentQueue) SeekToEnd(readerName string) error {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	files, err := pq.listQueueFiles()
	if err != nil {
		return err
	}

	state := readerState{}
	if len(files) > 0 {
		latestFile := files[len(files)-1]
		fullPath := filepath.Join(pq.dir, latestFile)
		info, err := pq.osProvider.Stat(fullPath)
		if err != nil {
			return err
		}
		state.FileName = latestFile
		state.Offset = info.Size()
	}

	return pq.saveState(readerName, state)
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
	if strings.HasPrefix(readerName, "ws_") {
		return pq.wsStates[readerName], nil
	}
	var state readerState
	stateFile := filepath.Join(pq.dir, fmt.Sprintf("reader_state_%s_%s.json", pq.name, readerName))
	if _, err := pq.osProvider.Stat(stateFile); os.IsNotExist(err) {
		return state, nil
	}
	f, err := pq.osProvider.OpenFile(stateFile, os.O_RDONLY, 0644)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	//goland:noinspection GoUnhandledErrorResult
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
	if strings.HasPrefix(readerName, "ws_") {
		pq.wsStates[readerName] = state
		return nil
	}
	stateFile := filepath.Join(pq.dir, fmt.Sprintf("reader_state_%s_%s.json", pq.name, readerName))
	// Use a temporary file for atomic write if possible, but here we just overwrite
	f, err := pq.osProvider.OpenFile(stateFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	//goland:noinspection GoUnhandledErrorResult
	defer f.Close()

	return json.NewEncoder(f).Encode(state)
}

func (pq *PersistentQueue) listQueueFiles() ([]string, error) {
	if _, err := pq.osProvider.Stat(pq.dir); os.IsNotExist(err) {
		return nil, nil
	}
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
	//goland:noinspection GoUnhandledErrorResult
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
