//nolint:revive
package core

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// FileStateStore persists state to files under a data directory.
type FileStateStore struct {
	DataDir string
	os      OsProviderApi
}

// NewFileStateStore creates a FileStateStore rooted at the given dataDir.
func NewFileStateStore(dataDir string, os OsProviderApi) *FileStateStore {
	return &FileStateStore{
		DataDir: dataDir,
		os:      os,
	}
}

func (s *FileStateStore) getFilePath(key string) string {
	return filepath.Join(s.DataDir, fmt.Sprintf("state_%s.json", key))
}

// Save writes a value as JSON to the state file identified by key.
func (s *FileStateStore) Save(key string, value interface{}) error {
	path := s.getFilePath(key)

	err := s.os.MkdirAll(s.DataDir, 0755)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	f, err := s.os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("FileStateStore: failed to close file %s: %v", path, err)
		}
	}()

	_, err = f.WriteString(string(data))
	return err
}

// Load reads and decodes JSON state from the file identified by key into value.
func (s *FileStateStore) Load(key string, value interface{}) error {
	path := s.getFilePath(key)

	f, err := s.os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state yet, not an error
		}
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("FileStateStore: failed to close file %s: %v", path, err)
		}
	}()

	decoder := json.NewDecoder(f)
	return decoder.Decode(value)
}

// NoOpStateStore is a StateStore that performs no persistence (useful for tests).
type NoOpStateStore struct{}

func (s *NoOpStateStore) Save(_ string, _ interface{}) error { return nil }
func (s *NoOpStateStore) Load(_ string, _ interface{}) error { return nil }
