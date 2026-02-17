package cmd

import (
	"compress/gzip"
	"fmt"
	"io"
	"keyop/core"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

func NewSelfUpdateCmd(deps core.Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "selfupdate",
		Short: "Update keyop to the latest version",
		Long:  `Download the latest build from geekfarm.org and install it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSelfUpdate(deps)
		},
	}
}

func runSelfUpdate(deps core.Dependencies) error {
	return runSelfUpdateWithURL(deps, "https://www.geekfarm.org/wu/keyop/latest")
}

func runSelfUpdateWithURL(deps core.Dependencies, baseURL string) error {
	logger := deps.MustGetLogger()

	url := fmt.Sprintf("%s/keyop-%s-%s.gz", baseURL, runtime.GOOS, runtime.GOARCH)
	logger.Info("Downloading update", "url", url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download update: received status code %d", resp.StatusCode)
	}

	totalSize := resp.ContentLength
	lastPercent := -1.0
	progressReader := &progressReader{
		Reader: resp.Body,
		Total:  totalSize,
		OnProgress: func(current, total int64) {
			if total > 0 {
				percent := float64(current) / float64(total) * 100
				if percent-lastPercent >= 1.0 || current == total {
					logger.Info(fmt.Sprintf("Download progress: %.0f%% (%d/%d bytes)", percent, current, total))
					lastPercent = percent
				}
			} else {
				logger.Info(fmt.Sprintf("Download progress: %d bytes", current))
			}
		},
	}

	gzReader, err := gzip.NewReader(progressReader)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	return installUpdate(logger, gzReader, exePath)
}

func installUpdate(logger core.Logger, gzReader io.Reader, exePath string) error {
	// Create a temporary file for the new binary
	tmpFile, err := os.CreateTemp(filepath.Dir(exePath), "keyop-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()

	logger.Info("Decompressing update", "to", tmpFile.Name())
	if _, err := io.Copy(tmpFile, gzReader); err != nil {
		return fmt.Errorf("failed to decompress update: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	// Set executable permissions
	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		return fmt.Errorf("failed to set executable permissions: %w", err)
	}

	logger.Info("Replacing current binary", "path", exePath)

	// Move the temporary file to the executable path
	if err := os.Rename(tmpFile.Name(), exePath); err != nil {
		return fmt.Errorf("failed to replace executable: %w (try running with sudo?)", err)
	}

	logger.Info("Update successful")
	return nil
}

type progressReader struct {
	io.Reader
	Total      int64
	Current    int64
	OnProgress func(current, total int64)
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.Reader.Read(p)
	pr.Current += int64(n)
	if pr.OnProgress != nil {
		pr.OnProgress(pr.Current, pr.Total)
	}
	return
}
