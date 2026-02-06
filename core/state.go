package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type FileStateStore struct {
	DataDir string
	os      OsProviderApi
}

func NewFileStateStore(dataDir string, os OsProviderApi) *FileStateStore {
	return &FileStateStore{
		DataDir: dataDir,
		os:      os,
	}
}

func (s *FileStateStore) getFilePath(key string) string {
	return filepath.Join(s.DataDir, fmt.Sprintf("state_%s.json", key))
}

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
	defer f.Close()

	_, err = f.WriteString(string(data))
	return err
}

func (s *FileStateStore) Load(key string, value interface{}) error {
	path := s.getFilePath(key)

	f, err := s.os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No state yet, not an error
		}
		return err
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	return decoder.Decode(value)
}
