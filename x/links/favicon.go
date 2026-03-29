package links

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// FetchAndCacheFavicon fetches a favicon for the given domain and caches it locally.
// It tries:
//  1. https://domain/favicon.ico
//  2. GET https://domain/ and parse HTML for <link rel="icon">
//
// Returns the relative path to store in the database, or empty string on failure.
// The favicon is stored at dataDir/favicons/<hash>.<ext>.
// This is non-blocking — failures are silently ignored.
func FetchAndCacheFavicon(domain, dataDir string) (string, error) {
	if domain == "" {
		return "", nil
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout: 5 * time.Second,
			}).Dial,
		},
	}

	// Try favicon.ico first
	iconURL := "https://" + domain + "/favicon.ico"
	if data, err := fetchBinaryURL(client, iconURL); err == nil && len(data) > 0 {
		return saveFavicon(data, domain, "ico", dataDir)
	}

	// Try to parse HTML from root and find icon link
	if data, err := fetchHTML(client, "https://"+domain+"/"); err == nil {
		if iconURL, ext := parseIconFromHTML(string(data)); iconURL != "" {
			// Resolve relative URLs
			if !strings.HasPrefix(iconURL, "http") {
				u, _ := url.Parse("https://" + domain + "/")
				if u != nil {
					rel, _ := url.Parse(iconURL)
					iconURL = u.ResolveReference(rel).String()
				}
			}
			if iconData, err := fetchBinaryURL(client, iconURL); err == nil && len(iconData) > 0 {
				return saveFavicon(iconData, domain, ext, dataDir)
			}
		}
	}

	return "", nil
}

// fetchBinaryURL fetches binary data from a URL with timeout.
func fetchBinaryURL(client *http.Client, iconURL string) ([]byte, error) {
	resp, err := client.Get(iconURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// fetchHTML fetches HTML from a URL.
func fetchHTML(client *http.Client, pageURL string) ([]byte, error) {
	resp, err := client.Get(pageURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// parseIconFromHTML parses the <head> for a favicon link. Returns (href, ext).
func parseIconFromHTML(html string) (string, string) {
	// Simple regex to find <link rel="icon"> or <link rel="shortcut icon">
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)<link[^>]*rel=["']icon["'][^>]*href=["']([^"']+)["'][^>]*>`),
		regexp.MustCompile(`(?i)<link[^>]*rel=["']shortcut icon["'][^>]*href=["']([^"']+)["'][^>]*>`),
		regexp.MustCompile(`(?i)<link[^>]*href=["']([^"']+)["'][^>]*rel=["']icon["'][^>]*>`),
		regexp.MustCompile(`(?i)<link[^>]*href=["']([^"']+)["'][^>]*rel=["']shortcut icon["'][^>]*>`),
	}

	for _, pat := range patterns {
		matches := pat.FindStringSubmatch(html)
		if len(matches) > 1 {
			href := matches[1]
			ext := "ico"
			if strings.Contains(href, ".png") {
				ext = "png"
			} else if strings.Contains(href, ".svg") {
				ext = "svg"
			} else if strings.Contains(href, ".webp") {
				ext = "webp"
			}
			return href, ext
		}
	}
	return "", ""
}

// saveFavicon writes favicon data to disk and returns the relative path.
func saveFavicon(data []byte, domain, ext, dataDir string) (string, error) {
	faviconDir := filepath.Join(dataDir, "favicons")
	if err := os.MkdirAll(faviconDir, 0750); err != nil {
		return "", err
	}

	filename := hashDomain(domain) + "." + ext
	path := filepath.Join(faviconDir, filename) // #nosec - filename is hash-derived, safe

	// #nosec G703 - path is hash-derived and safe
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}

	// Return relative path for storage in DB
	return filename, nil
}
