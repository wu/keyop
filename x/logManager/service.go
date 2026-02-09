package logManager

import (
	"compress/gzip"
	"fmt"
	"io"
	"keyop/core"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Service struct {
	Deps core.Dependencies
	Cfg  core.ServiceConfig
}

func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	return Service{
		Deps: deps,
		Cfg:  cfg,
	}
}

func (svc Service) ValidateConfig() []error {
	return nil
}

func (svc Service) Initialize() error {
	return nil
}

func (svc Service) Check() error {
	logger := svc.Deps.MustGetLogger()
	osProvider := svc.Deps.MustGetOsProvider()

	dataDir, _ := svc.Cfg.Config["dataDir"].(string)
	if dataDir == "" {
		dataDir = "data"
	}

	logger.Info("Checking for old log files", "dir", dataDir)

	files, err := osProvider.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn("Data directory does not exist", "dir", dataDir)
			return nil
		}
		return fmt.Errorf("failed to read data directory: %w", err)
	}

	now := time.Now()
	maxAge := 48 * time.Hour
	if maxAgeStr, ok := svc.Cfg.Config["maxAge"].(string); ok {
		if dur, err := time.ParseDuration(maxAgeStr); err == nil {
			maxAge = dur
		} else {
			logger.Warn("Failed to parse maxAge, using default", "maxAge", maxAgeStr, "error", err, "default", maxAge)
		}
	}
	threshold := now.Add(-maxAge)

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		name := file.Name()
		// Only process .log files that are not already gzipped
		if !strings.HasSuffix(name, ".log") {
			continue
		}

		filePath := filepath.Join(dataDir, name)
		info, err := osProvider.Stat(filePath)
		if err != nil {
			logger.Error("Failed to stat file", "file", filePath, "error", err)
			continue
		}

		if info.ModTime().Before(threshold) {
			logger.Info("Compressing old log file", "file", name, "modTime", info.ModTime())
			if err := svc.gzipFile(filePath, info.ModTime()); err != nil {
				logger.Error("Failed to gzip file", "file", filePath, "error", err)
				continue
			}
			if err := osProvider.Remove(filePath); err != nil {
				logger.Error("Failed to remove original file after compression", "file", filePath, "error", err)
			}
		}
	}

	return nil
}

func (svc Service) gzipFile(srcPath string, modTime time.Time) error {
	osProvider := svc.Deps.MustGetOsProvider()
	destPath := srcPath + ".gz"

	src, err := osProvider.OpenFile(srcPath, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer src.Close()

	dest, err := osProvider.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer dest.Close()

	gz := gzip.NewWriter(dest)
	gz.ModTime = modTime
	defer gz.Close()

	if _, err := io.Copy(gz, src); err != nil {
		return err
	}

	if err := gz.Close(); err != nil {
		return err
	}

	if err := dest.Close(); err != nil {
		return err
	}

	return osProvider.Chtimes(destPath, modTime, modTime)
}
