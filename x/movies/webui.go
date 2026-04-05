package movies

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"keyop/x/webui"
)

//go:embed resources
var embeddedAssets embed.FS

// WebUIAssets returns the static assets for the movies service.
func (svc *Service) WebUIAssets() http.FileSystem {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return http.FS(sub)
}

// WebUITab implements webui.TabProvider.
func (svc *Service) WebUITab() webui.TabInfo {
	htmlContent, err := embeddedAssets.ReadFile("resources/movies.html")
	if err != nil {
		htmlContent = []byte(`<div id="movies-container">Movies tab failed to load.</div>`)
	}

	cssContent, err := embeddedAssets.ReadFile("resources/movies.css")
	if err != nil {
		cssContent = []byte{}
	}

	content := string(htmlContent) + "\n<style>\n" + string(cssContent) + "\n</style>"

	return webui.TabInfo{
		ID:      "movies",
		Title:   "🎥",
		Content: content,
		JSPath:  "/api/assets/movies/movies.js",
	}
}

// HandleWebUIAction dispatches WebUI actions to the appropriate handler.
func (svc *Service) HandleWebUIAction(action string, params map[string]any) (any, error) {
	switch action {
	case "list-movies":
		return svc.actionListMovies(params)
	case "get-movie":
		return svc.actionGetMovie(params)
	case "create-movie":
		return svc.actionCreateMovie(params)
	case "update-movie":
		return svc.actionUpdateMovie(params)
	case "delete-movie":
		return svc.actionDeleteMovie(params)
	case "import-nfo":
		return svc.actionImportNFO(params)
	case "search-tmdb":
		return svc.actionSearchTMDB(params)
	case "fetch-tmdb":
		return svc.actionFetchTMDB(params)
	case "list-movies-by-actor":
		return svc.actionListMoviesByActor(params)
	case "get-tag-counts":
		return svc.actionGetTagCounts(params)
	case "get-state":
		return svc.getState()
	case "save-state":
		return svc.saveState(params)
	default:
		return nil, fmt.Errorf("movies: unknown action: %s", action)
	}
}

// RegisterRoutes registers the image serving and upload routes on the webui mux.
func (svc *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/movies/image/{uuid}/{kind}", svc.handleMovieImage)
	mux.HandleFunc("POST /api/movies/upload-image", svc.handleUploadMovieImage)
}

// handleMovieImage serves a locally-stored movie image (poster or fanart).
func (svc *Service) handleMovieImage(w http.ResponseWriter, r *http.Request) {
	movieUUID := r.PathValue("uuid")
	kind := r.PathValue("kind")

	// Validate to prevent path traversal — UUIDs contain only hex + dashes,
	// kind is one of "poster" / "fanart".
	if strings.ContainsAny(movieUUID+kind, "/\\.") || kind == "" || movieUUID == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	pattern := filepath.Join(svc.imagesDir, movieUUID+"_"+kind+".*")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	f, err := os.Open(matches[0]) //nolint:gosec
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close() //nolint:errcheck

	fi, err := f.Stat()
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	ct := mime.TypeByExtension(filepath.Ext(matches[0]))
	if ct == "" {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	// Images are keyed by UUID and never change once written — cache aggressively.
	// "immutable" tells modern browsers not to revalidate on reload.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, filepath.Base(matches[0]), fi.ModTime(), f)
}

// handleUploadMovieImage accepts a multipart POST with fields "uuid" and "file",
// saves the image to local storage, updates the DB poster_url, and returns the new URL.
func (svc *Service) handleUploadMovieImage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	movieUUID := strings.TrimSpace(r.FormValue("uuid"))
	if movieUUID == "" || strings.ContainsAny(movieUUID, "/\\.") {
		http.Error(w, "missing or invalid uuid", http.StatusBadRequest)
		return
	}

	f, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer f.Close() //nolint:errcheck

	// Derive extension from content-type or filename.
	ct := header.Header.Get("Content-Type")
	ext := imageExtFromContentType(ct)
	if ext == "" {
		ext = filepath.Ext(header.Filename)
		if ext == "" {
			ext = ".jpg"
		}
	}

	// Validate it's an image extension.
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif":
	default:
		http.Error(w, "unsupported image type", http.StatusBadRequest)
		return
	}

	// Look up the movie so we have its DB id.
	m, err := getMovieByUUID(svc.db, movieUUID)
	if err != nil {
		http.Error(w, "movie not found", http.StatusNotFound)
		return
	}

	// Delete any existing poster files for this movie.
	pattern := filepath.Join(svc.imagesDir, movieUUID+"_poster.*")
	if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
		for _, p := range matches {
			_ = os.Remove(p) //nolint:gosec
		}
	}

	// Write new file.
	filename := movieUUID + "_poster" + ext
	destPath := filepath.Join(svc.imagesDir, filename)
	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640) //nolint:gosec
	if err != nil {
		http.Error(w, "failed to save file", http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(out, f); err != nil {
		_ = out.Close()
		_ = os.Remove(destPath) //nolint:gosec
		http.Error(w, "failed to write file", http.StatusInternalServerError)
		return
	}
	_ = out.Close()

	localURL := "/api/movies/image/" + movieUUID + "/poster"
	if err := updateMoviePoster(svc.db, m.ID, localURL); err != nil {
		http.Error(w, "failed to update db", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"poster_url": localURL})
}

// getState retrieves UI state from the state store.
func (svc *Service) getState() (any, error) {
	// Create a temporary state store for movies data
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return map[string]any{}, nil
	}
	dataDir := filepath.Join(homeDir, ".keyop", "data", "movies")
	path := filepath.Join(dataDir, "ui_state.json")

	// #nosec G304 - path is constructed from fixed home directory and literal filename
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

// saveState saves UI state to the state store.
func (svc *Service) saveState(params map[string]any) (any, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dataDir := filepath.Join(homeDir, ".keyop", "data", "movies")
	path := filepath.Join(dataDir, "ui_state.json")

	data, err := json.MarshalIndent(params, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	return map[string]string{"status": "ok"}, nil
}
