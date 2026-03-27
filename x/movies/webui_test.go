package movies

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestMux builds a ServeMux with the movie image routes registered.
func newTestMux(svc *Service) *http.ServeMux {
	mux := http.NewServeMux()
	svc.RegisterRoutes(mux)
	return mux
}

// ── handleMovieImage ──────────────────────────────────────────────────────────

func TestHandleMovieImage_NotFound(t *testing.T) {
	svc := newTestMoviesService(t)
	mux := newTestMux(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/movies/image/nonexistent-uuid/poster", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleMovieImage_Success(t *testing.T) {
	svc := newTestMoviesService(t)

	// Insert a movie and write a fake image file named after its UUID.
	id := mustInsert(t, svc.db, movie{Title: "Poster Test", Year: 2000})
	m, err := getMovie(svc.db, id)
	if err != nil {
		t.Fatalf("getMovie: %v", err)
	}

	imgPath := filepath.Join(svc.imagesDir, m.UUID+"_poster.jpg")
	if err := os.WriteFile(imgPath, []byte("fake-jpeg-data"), 0600); err != nil {
		t.Fatalf("write test image: %v", err)
	}

	mux := newTestMux(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/movies/image/"+m.UUID+"/poster", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "fake-jpeg-data" {
		t.Errorf("unexpected body: %q", got)
	}
}

func TestHandleMovieImage_InvalidPath(t *testing.T) {
	svc := newTestMoviesService(t)
	mux := newTestMux(svc)

	// A UUID with a path-traversal character should be rejected.
	req := httptest.NewRequest(http.MethodGet, "/api/movies/image/../../etc/poster", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Either 400 or 404 is acceptable — just not 200.
	if w.Code == http.StatusOK {
		t.Errorf("should not serve a response for path-traversal attempt; got 200")
	}
}

// ── handleUploadMovieImage ────────────────────────────────────────────────────

// buildMultipartRequest creates a multipart/form-data POST with optional uuid
// and file fields for testing the upload handler.
func buildMultipartRequest(t *testing.T, uuid, filename, contentType string, fileData []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if uuid != "" {
		if err := w.WriteField("uuid", uuid); err != nil {
			t.Fatalf("write uuid field: %v", err)
		}
	}

	if filename != "" {
		// Create a file part with an explicit Content-Type header.
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
		if contentType != "" {
			h.Set("Content-Type", contentType)
		}
		part, err := w.CreatePart(h)
		if err != nil {
			t.Fatalf("create file part: %v", err)
		}
		if _, err := io.Copy(part, bytes.NewReader(fileData)); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/movies/upload-image", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestHandleUploadMovieImage_MissingUUID(t *testing.T) {
	svc := newTestMoviesService(t)
	mux := newTestMux(svc)

	req := buildMultipartRequest(t, "", "poster.jpg", "image/jpeg", []byte("data"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing uuid, got %d", w.Code)
	}
}

func TestHandleUploadMovieImage_UnknownMovie(t *testing.T) {
	svc := newTestMoviesService(t)
	mux := newTestMux(svc)

	req := buildMultipartRequest(t, "no-such-uuid", "poster.jpg", "image/jpeg", []byte("data"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown uuid, got %d", w.Code)
	}
}

func TestHandleUploadMovieImage_InvalidFileType(t *testing.T) {
	svc := newTestMoviesService(t)

	// Insert a real movie so UUID lookup succeeds.
	id := mustInsert(t, svc.db, movie{Title: "Upload Test"})
	m, _ := getMovie(svc.db, id)

	mux := newTestMux(svc)
	req := buildMultipartRequest(t, m.UUID, "malware.exe", "application/octet-stream", []byte("exec"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported file type, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unsupported") {
		t.Errorf("expected 'unsupported' in response, got: %s", w.Body.String())
	}
}

func TestHandleUploadMovieImage_Success(t *testing.T) {
	svc := newTestMoviesService(t)

	// Insert a movie and obtain its UUID.
	id := mustInsert(t, svc.db, movie{Title: "Upload Success"})
	m, _ := getMovie(svc.db, id)

	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0} // minimal JPEG header bytes

	mux := newTestMux(svc)
	req := buildMultipartRequest(t, m.UUID, "poster.jpg", "image/jpeg", fakeJPEG)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Response body should contain the local poster URL.
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantURL := "/api/movies/image/" + m.UUID + "/poster"
	if resp["poster_url"] != wantURL {
		t.Errorf("poster_url: want %q, got %q", wantURL, resp["poster_url"])
	}

	// Verify the file was actually written to imagesDir.
	pattern := filepath.Join(svc.imagesDir, m.UUID+"_poster.*")
	matches, _ := filepath.Glob(pattern)
	if len(matches) != 1 {
		t.Errorf("expected 1 poster file saved, found %d matching %s", len(matches), pattern)
	}

	// Verify the DB was updated.
	updated, _ := getMovie(svc.db, id)
	if updated.PosterURL != wantURL {
		t.Errorf("DB poster_url: want %q, got %q", wantURL, updated.PosterURL)
	}
}

func TestHandleUploadMovieImage_ReplacesExisting(t *testing.T) {
	svc := newTestMoviesService(t)

	id := mustInsert(t, svc.db, movie{Title: "Replace Poster"})
	m, _ := getMovie(svc.db, id)

	// Write an old poster file.
	oldPath := filepath.Join(svc.imagesDir, m.UUID+"_poster.png")
	if err := os.WriteFile(oldPath, []byte("old"), 0600); err != nil {
		t.Fatalf("write old poster: %v", err)
	}

	mux := newTestMux(svc)
	req := buildMultipartRequest(t, m.UUID, "new.jpg", "image/jpeg", []byte("new-data"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Old PNG should be gone.
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old poster PNG should have been removed")
	}

	// New JPG should exist.
	newPath := filepath.Join(svc.imagesDir, m.UUID+"_poster.jpg")
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new poster JPG should exist: %v", err)
	}
}
