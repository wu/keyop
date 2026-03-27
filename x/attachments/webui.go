package attachments

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"keyop/x/webui"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUIAssets returns the static assets for the attachments service.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUITab returns the tab configuration for the attachments service.
func (svc *Service) WebUITab() webui.TabInfo {
	return webui.TabInfo{
		ID:    "attachments",
		Title: "📎",
		Content: `<div id="attachments-container">
<div class="attachments-layout">
  <div class="attachments-content">
    <div id="attachments-list">Loading...</div>
  </div>
</div>
</div>`,
		JSPath: "/api/assets/attachments/attachments.js",
	}
}

// HandleWebUIAction handles JSON actions from the WebUI.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "list-files":
		return svc.listFiles()
	case "delete-file":
		if params == nil {
			return map[string]any{"error": "missing params"}, nil
		}
		id, ok := params["id"].(string)
		if !ok || id == "" {
			return map[string]any{"error": "invalid id"}, nil
		}
		return svc.deleteFile(id)
	default:
		return nil, nil
	}
}

// RegisterRoutes registers custom HTTP routes on the webui mux.
func (svc *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/attachments/upload", svc.handleUpload)
	mux.HandleFunc("GET /api/attachments/file/{uuid}", svc.handleServeFile)
	mux.HandleFunc("GET /api/attachments/preview/{uuid}", svc.handlePreviewFile)
}

// listFiles returns all attachment records as JSON-serialisable maps.
func (svc *Service) listFiles() (any, error) {
	if svc.db == nil {
		return map[string]any{"files": []any{}}, nil
	}
	attachments, err := listAttachments(svc.db)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	files := make([]map[string]any, 0, len(attachments))
	for _, a := range attachments {
		files = append(files, map[string]any{
			"id":               a.UUID,
			"originalFilename": a.OriginalFilename,
			"storedFilename":   a.StoredFilename,
			"dateDir":          a.DateDir,
			"mimeType":         a.MimeType,
			"size":             a.Size,
			"uploadedAt":       a.UploadedAt.Format(time.RFC3339),
		})
	}
	return map[string]any{"files": files}, nil
}

// deleteFile removes the file from disk and the database by UUID.
func (svc *Service) deleteFile(id string) (any, error) {
	if svc.db == nil {
		return map[string]any{"error": "database not initialised"}, nil
	}
	a, err := getAttachmentByUUID(svc.db, id)
	if err != nil {
		return map[string]any{"error": "file not found"}, nil
	}
	path := filepath.Join(svc.uploadDir, a.DateDir, a.StoredFilename)
	_ = os.Remove(path)
	if err := deleteAttachmentByUUID(svc.db, id); err != nil {
		return nil, fmt.Errorf("delete attachment record: %w", err)
	}
	return map[string]any{"deleted": true}, nil
}

// handleUpload handles multipart file upload POST requests.
func (svc *Service) handleUpload(w http.ResponseWriter, r *http.Request) {
	logger := svc.Deps.MustGetLogger()

	if err := r.ParseMultipartForm(64 << 20); err != nil { // 64 MiB
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close() //nolint:errcheck

	// Determine stored filename: use the user-supplied name (from the modal prompt),
	// falling back to the original filename. Always sanitize server-side for safety.
	chosenName := r.FormValue("filename")
	if chosenName == "" {
		chosenName = header.Filename
	}
	storedFilename := sanitizeFilename(chosenName)

	// Create date-based sub-directory.
	dateDir := todayDir()
	destDir := filepath.Join(svc.uploadDir, dateDir)
	if err := os.MkdirAll(destDir, 0750); err != nil { //nolint:gosec // destDir is svc.uploadDir (configured path) + server-generated date string, not user input
		logger.Error("attachments: failed to create date dir", "path", destDir, "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	destPath := filepath.Join(destDir, storedFilename)
	// If a file with the same name already exists, append a counter.
	destPath = uniquePath(destPath)
	storedFilename = filepath.Base(destPath)

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0640) //nolint:gosec
	if err != nil {
		logger.Error("attachments: failed to create file", "path", destPath, "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	written, err := io.Copy(out, file)
	_ = out.Close()
	if err != nil {
		logger.Error("attachments: failed to write file", "path", destPath, "error", err)
		_ = os.Remove(destPath) //nolint:gosec
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(storedFilename))
	}

	rec := attachment{
		UUID:             uuid.New().String(),
		OriginalFilename: header.Filename,
		StoredFilename:   storedFilename,
		DateDir:          dateDir,
		MimeType:         mimeType,
		Size:             written,
		UploadedAt:       time.Now().UTC(),
	}
	_, err = insertAttachment(svc.db, rec)
	if err != nil {
		logger.Error("attachments: failed to insert record", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":               rec.UUID,
		"originalFilename": rec.OriginalFilename,
		"storedFilename":   storedFilename,
		"dateDir":          dateDir,
		"mimeType":         mimeType,
		"size":             written,
		"uploadedAt":       rec.UploadedAt.Format(time.RFC3339),
	})
}

// handleServeFile serves a stored attachment by UUID (forces download).
func (svc *Service) handleServeFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("uuid")
	if id == "" {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	a, err := getAttachmentByUUID(svc.db, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	path := filepath.Join(svc.uploadDir, a.DateDir, a.StoredFilename)
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		http.Error(w, "file not found on disk", http.StatusNotFound)
		return
	}
	defer f.Close() //nolint:errcheck

	if a.MimeType != "" {
		w.Header().Set("Content-Type", a.MimeType)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, a.OriginalFilename))
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, a.StoredFilename, fi.ModTime(), f)
}

// handlePreviewFile serves a stored attachment inline (for in-browser preview).
func (svc *Service) handlePreviewFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("uuid")
	if id == "" {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	a, err := getAttachmentByUUID(svc.db, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	path := filepath.Join(svc.uploadDir, a.DateDir, a.StoredFilename)
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		http.Error(w, "file not found on disk", http.StatusNotFound)
		return
	}
	defer f.Close() //nolint:errcheck

	if a.MimeType != "" {
		w.Header().Set("Content-Type", a.MimeType)
	}
	// inline: let the browser display rather than download
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, a.OriginalFilename))
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, a.StoredFilename, fi.ModTime(), f)
}

// uniquePath appends _1, _2, … to the base name when a file already exists.
func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) { //nolint:gosec
		return path
	}
	ext := filepath.Ext(path)
	base := path[:len(path)-len(ext)]
	for i := 1; i < 1000; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) { //nolint:gosec
			return candidate
		}
	}
	return path
}
