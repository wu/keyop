package movies

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"keyop/core"

	"github.com/google/uuid"
)

const tmdbBaseURL = "https://api.themoviedb.org/3"
const tmdbImageBase = "https://image.tmdb.org/t/p/w500"

// Service manages movie library data.
type Service struct {
	Deps       core.Dependencies
	Cfg        core.ServiceConfig
	db         *sql.DB
	dbPath     string
	imagesDir  string
	tmdbAPIKey string
	httpClient *http.Client
}

// NewService constructs the movies service.
func NewService(deps core.Dependencies, cfg core.ServiceConfig) core.Service {
	dbPath := "~/.keyop/sqlite/movies.sql"
	if d, ok := cfg.Config["dbPath"].(string); ok && d != "" {
		dbPath = d
	}
	imagesDir := "~/.keyop/movies/images"
	if d, ok := cfg.Config["imagesDir"].(string); ok && d != "" {
		imagesDir = d
	}
	tmdbAPIKey := ""
	if k, ok := cfg.Config["tmdbApiKey"].(string); ok {
		tmdbAPIKey = k
	}
	return &Service{
		Deps:       deps,
		Cfg:        cfg,
		dbPath:     dbPath,
		imagesDir:  imagesDir,
		tmdbAPIKey: tmdbAPIKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// ValidateConfig performs minimal validation.
func (svc *Service) ValidateConfig() []error {
	return nil
}

// Check returns nil if the database is initialized.
func (svc *Service) Check() error {
	if svc.db == nil {
		return fmt.Errorf("movies: database not initialized")
	}
	return nil
}

// Initialize opens the SQLite database and runs the schema.
func (svc *Service) Initialize() error {
	dbPath := expandHome(svc.dbPath)
	svc.dbPath = dbPath

	if err := os.MkdirAll(filepath.Dir(dbPath), 0750); err != nil {
		return fmt.Errorf("movies: failed to create db dir: %w", err)
	}

	imagesDir := expandHome(svc.imagesDir)
	svc.imagesDir = imagesDir
	if err := os.MkdirAll(imagesDir, 0750); err != nil {
		return fmt.Errorf("movies: failed to create images dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return fmt.Errorf("movies: failed to open database: %w", err)
	}
	svc.db = db

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("movies: failed to enable foreign keys: %w", err)
	}

	if _, err := db.Exec(moviesSchema); err != nil {
		return fmt.Errorf("movies: failed to create schema: %w", err)
	}
	if err := migrateMoviesSchema(db); err != nil {
		return fmt.Errorf("movies: failed to migrate schema: %w", err)
	}

	// Subscribe to the 'movie' channel to receive MovieWatchedEvent messages.
	movieChan, ok := svc.Cfg.Subs["movie"]
	if ok {
		messenger := svc.Deps.MustGetMessenger()
		if err := messenger.Subscribe(
			svc.Deps.MustGetContext(),
			svc.Cfg.Name,
			movieChan.Name,
			svc.Cfg.Type,
			svc.Cfg.Name,
			movieChan.MaxAge,
			svc.handleMovieWatched,
		); err != nil {
			return fmt.Errorf("movies: failed to subscribe to movie channel: %w", err)
		}
	}

	return nil
}

// handleMovieWatched processes an incoming MovieWatchedEvent: updates last_played
// on the matching movie and persists the watch event to movie_watch_events.
func (svc *Service) handleMovieWatched(msg core.Message) error {
	logger := svc.Deps.MustGetLogger()

	var evt MovieWatchedEvent
	switch d := msg.Data.(type) {
	case *MovieWatchedEvent:
		evt = *d
	case MovieWatchedEvent:
		evt = d
	case map[string]interface{}:
		// Fallback: decode from raw map.
		if t, ok := d["title"].(string); ok {
			evt.Title = t
		}
		if w, ok := d["watchedAt"].(string); ok {
			evt.WatchedAt, _ = time.Parse(time.RFC3339, w)
		}
	default:
		logger.Warn("movies: unexpected payload type for MovieWatchedEvent", "type", fmt.Sprintf("%T", msg.Data))
		return nil
	}

	if evt.Title == "" {
		return nil
	}
	if evt.WatchedAt.IsZero() {
		evt.WatchedAt = time.Now()
	}

	updated, err := updateMovieLastPlayed(svc.db, evt.Title, evt.WatchedAt)
	if err != nil {
		logger.Error("movies: failed to update last_played", "title", evt.Title, "error", err)
	} else if updated {
		logger.Info("movies: updated last_played", "title", evt.Title)
	} else {
		logger.Warn("movies: movie not found for watched event", "title", evt.Title)
		messenger := svc.Deps.MustGetMessenger()
		alert := core.AlertEvent{
			Summary: fmt.Sprintf("Movie watched but not in library: %s", evt.Title),
			Text:    fmt.Sprintf("Received a Movie Watched event for \"%s\" but no matching title was found in the movie library.", evt.Title),
			Level:   "warning",
		}
		_ = messenger.Send(core.Message{
			ChannelName: svc.Cfg.Name,
			ServiceName: svc.Cfg.Name,
			ServiceType: svc.Cfg.Type,
			Event:       "movie.watched.not-found",
			Text:        alert.Summary,
			Data:        alert,
		})
	}

	if err := insertWatchEvent(svc.db, evt.Title, evt.WatchedAt); err != nil {
		logger.Error("movies: failed to insert watch event", "title", evt.Title, "error", err)
	}

	return nil
}

// localizeImage downloads an external image URL to local storage and returns
// the local serve path. If the URL is already local or empty, it's returned
// unchanged. On failure, logs the error and returns an empty string.
func (svc *Service) localizeImage(rawURL, movieUUID, kind string) string {
	if rawURL == "" {
		return ""
	}
	if !strings.HasPrefix(rawURL, "http") {
		return rawURL // already a local path
	}

	resp, err := svc.httpClient.Get(rawURL) //nolint:noctx
	if err != nil {
		svc.Deps.MustGetLogger().Warn("movies: image download failed", "kind", kind, "url", rawURL, "error", err)
		return ""
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		svc.Deps.MustGetLogger().Warn("movies: image download non-200", "kind", kind, "status", resp.StatusCode)
		return ""
	}

	ext := imageExtFromContentType(resp.Header.Get("Content-Type"))
	if ext == "" {
		rawPath := rawURL
		if idx := strings.Index(rawPath, "?"); idx >= 0 {
			rawPath = rawPath[:idx]
		}
		ext = filepath.Ext(rawPath)
		if ext == "" {
			ext = ".jpg"
		}
	}

	filename := movieUUID + "_" + kind + ext
	destPath := filepath.Join(svc.imagesDir, filename)

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640) //nolint:gosec
	if err != nil {
		svc.Deps.MustGetLogger().Warn("movies: image file create failed", "path", destPath, "error", err)
		return ""
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(destPath)
		svc.Deps.MustGetLogger().Warn("movies: image write failed", "path", destPath, "error", err)
		return ""
	}
	_ = f.Close()

	return "/api/movies/image/" + movieUUID + "/" + kind
}

// deleteMovieImages removes all locally stored image files for a movie.
func (svc *Service) deleteMovieImages(movieUUID string) {
	for _, kind := range []string{"poster", "fanart"} {
		pattern := filepath.Join(svc.imagesDir, movieUUID+"_"+kind+".*")
		matches, _ := filepath.Glob(pattern)
		for _, p := range matches {
			_ = os.Remove(p)
		}
	}
}

// deleteImageFiles removes locally stored image files for a specific kind.
func (svc *Service) deleteImageFiles(movieUUID, kind string) {
	pattern := filepath.Join(svc.imagesDir, movieUUID+"_"+kind+".*")
	matches, _ := filepath.Glob(pattern)
	for _, p := range matches {
		_ = os.Remove(p)
	}
}

// imageExtFromContentType returns the file extension for a MIME type.
func imageExtFromContentType(ct string) string {
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = ct[:idx]
	}
	switch strings.TrimSpace(strings.ToLower(ct)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	return ""
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// versionedImageURL appends a ?v= cache-busting param to local image URLs so
// that browsers re-fetch when a poster is replaced (updated_at changes) while
// still caching unchanged images indefinitely. Any existing ?v= param is
// stripped first to prevent double-versioning if a versioned URL was stored.
func versionedImageURL(url string, updatedAt time.Time) string {
	if url == "" || !strings.HasPrefix(url, "/") {
		return url
	}
	return fmt.Sprintf("%s?v=%d", stripVersionParam(url), updatedAt.Unix())
}

// stripVersionParam removes the ?v= cache-busting query param from a local
// image URL so that the clean path can be stored in the DB and compared.
func stripVersionParam(url string) string {
	if i := strings.Index(url, "?v="); i != -1 {
		return url[:i]
	}
	return url
}

// movieToMap converts a movie struct to a map for JSON serialization.
func movieToMap(m movie) map[string]any {
	actors := make([]map[string]any, len(m.Actors))
	for i, a := range m.Actors {
		actors[i] = map[string]any{
			"id":         a.ID,
			"movie_id":   a.MovieID,
			"name":       a.Name,
			"role":       a.Role,
			"sort_order": a.SortOrder,
		}
	}
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	return map[string]any{
		"id":          m.ID,
		"uuid":        m.UUID,
		"title":       m.Title,
		"sort_title":  m.SortTitle,
		"year":        m.Year,
		"plot":        m.Plot,
		"tagline":     m.Tagline,
		"runtime":     m.Runtime,
		"rating":      m.Rating,
		"tmdb_id":     m.TmdbID,
		"imdb_id":     m.ImdbID,
		"poster_url":  versionedImageURL(m.PosterURL, m.UpdatedAt),
		"fanart_url":  versionedImageURL(m.FanartURL, m.UpdatedAt),
		"set_name":    m.SetName,
		"last_played": m.LastPlayed,
		"tags":        tags,
		"actors":      actors,
		"created_at":  m.CreatedAt.Format(time.RFC3339),
		"updated_at":  m.UpdatedAt.Format(time.RFC3339),
	}
}

func (svc *Service) actionListMovies(params map[string]any) (any, error) {
	tag, _ := params["tag"].(string)
	tag = strings.ToLower(strings.TrimSpace(tag))
	search, _ := params["search"].(string)
	setName, _ := params["set_name"].(string)
	sort, _ := params["sort"].(string)
	fulltext, _ := params["fulltext"].(bool)
	movies, err := listMovies(svc.db, tag, search, setName, sort, fulltext)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, len(movies))
	for i, m := range movies {
		result[i] = movieToMap(m)
	}
	return map[string]any{"movies": result}, nil
}

func (svc *Service) actionGetMovie(params map[string]any) (any, error) {
	idF, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing id")
	}
	m, err := getMovie(svc.db, int64(idF))
	if err != nil {
		return nil, err
	}
	return movieToMap(m), nil
}

func (svc *Service) actionCreateMovie(params map[string]any) (any, error) {
	m := movieFromParams(params)
	// Pre-generate UUID so we can name the image files before inserting.
	if m.UUID == "" {
		m.UUID = uuid.New().String()
	}
	m.PosterURL = svc.localizeImage(m.PosterURL, m.UUID, "poster")
	m.FanartURL = svc.localizeImage(m.FanartURL, m.UUID, "fanart")
	id, err := insertMovie(svc.db, m)
	if err != nil {
		return nil, err
	}
	created, err := getMovie(svc.db, id)
	if err != nil {
		return nil, err
	}
	return movieToMap(created), nil
}

func (svc *Service) actionUpdateMovie(params map[string]any) (any, error) {
	idF, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing id")
	}
	id := int64(idF)
	m := movieFromParams(params)
	m.ID = id

	// Preserve UUID and handle image changes relative to existing record.
	if existing, err := getMovie(svc.db, id); err == nil {
		m.UUID = existing.UUID
		if m.PosterURL != existing.PosterURL {
			svc.deleteImageFiles(m.UUID, "poster")
			m.PosterURL = svc.localizeImage(m.PosterURL, m.UUID, "poster")
		}
		if m.FanartURL != existing.FanartURL {
			svc.deleteImageFiles(m.UUID, "fanart")
			m.FanartURL = svc.localizeImage(m.FanartURL, m.UUID, "fanart")
		}
	}

	if err := updateMovie(svc.db, m); err != nil {
		return nil, err
	}
	updated, err := getMovie(svc.db, m.ID)
	if err != nil {
		return nil, err
	}
	return movieToMap(updated), nil
}

func (svc *Service) actionDeleteMovie(params map[string]any) (any, error) {
	idF, ok := params["id"].(float64)
	if !ok {
		return nil, fmt.Errorf("missing id")
	}
	id := int64(idF)
	// Clean up image files before removing the DB record.
	if m, err := getMovie(svc.db, id); err == nil {
		svc.deleteMovieImages(m.UUID)
	}
	if err := deleteMovie(svc.db, id); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (svc *Service) actionImportNFO(params map[string]any) (any, error) {
	files, _ := params["files"].([]any)
	var imported []map[string]any
	var errors []string
	skipped := 0

	for _, f := range files {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		name, _ := fm["name"].(string)
		content, _ := fm["content"].(string)

		m, err := parseNFO(content)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %s", name, err.Error()))
			continue
		}

		// Check for duplicate (same title + year).
		var existingID int64
		err = svc.db.QueryRow(
			`SELECT id FROM movies WHERE LOWER(title)=LOWER(?) AND year=?`, m.Title, m.Year,
		).Scan(&existingID)
		if err == nil {
			skipped++
			continue
		}

		// Pre-generate UUID so image files can be named before the DB insert.
		if m.UUID == "" {
			m.UUID = uuid.New().String()
		}
		m.PosterURL = svc.localizeImage(m.PosterURL, m.UUID, "poster")
		m.FanartURL = svc.localizeImage(m.FanartURL, m.UUID, "fanart")

		id, err := insertMovie(svc.db, m)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %s", name, err.Error()))
			continue
		}
		created, _ := getMovie(svc.db, id)
		imported = append(imported, movieToMap(created))
	}

	if imported == nil {
		imported = []map[string]any{}
	}
	if errors == nil {
		errors = []string{}
	}
	return map[string]any{
		"imported": imported,
		"skipped":  skipped,
		"errors":   errors,
	}, nil
}

func (svc *Service) actionSearchTMDB(params map[string]any) (any, error) {
	if svc.tmdbAPIKey == "" {
		return map[string]any{"error": "tmdb_api_key not configured"}, nil
	}
	query, _ := params["query"].(string)
	year, _ := params["year"].(string)

	u := fmt.Sprintf("%s/search/movie?api_key=%s&query=%s",
		tmdbBaseURL, svc.tmdbAPIKey, url.QueryEscape(query))
	if year != "" {
		u += "&year=" + url.QueryEscape(year)
	}

	resp, err := svc.httpClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("tmdb search: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tmdb search read: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("tmdb search parse: %w", err)
	}

	// Normalize poster paths to full URLs
	if results, ok := result["results"].([]any); ok {
		for _, r := range results {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			if pp, ok := rm["poster_path"].(string); ok && pp != "" {
				rm["poster_url"] = tmdbImageBase + pp
			}
		}
	}
	return result, nil
}

func (svc *Service) actionFetchTMDB(params map[string]any) (any, error) {
	if svc.tmdbAPIKey == "" {
		return map[string]any{"error": "tmdb_api_key not configured"}, nil
	}
	tmdbID, _ := params["tmdb_id"].(string)
	if tmdbID == "" {
		return nil, fmt.Errorf("missing tmdb_id")
	}

	// Fetch movie details
	movieURL := fmt.Sprintf("%s/movie/%s?api_key=%s", tmdbBaseURL, tmdbID, svc.tmdbAPIKey)
	mResp, err := svc.httpClient.Get(movieURL)
	if err != nil {
		return nil, fmt.Errorf("tmdb fetch movie: %w", err)
	}
	defer mResp.Body.Close() //nolint:errcheck
	mBody, err := io.ReadAll(mResp.Body)
	if err != nil {
		return nil, err
	}
	var mData map[string]any
	if err := json.Unmarshal(mBody, &mData); err != nil {
		return nil, err
	}

	// Fetch credits
	creditsURL := fmt.Sprintf("%s/movie/%s/credits?api_key=%s", tmdbBaseURL, tmdbID, svc.tmdbAPIKey)
	cResp, err := svc.httpClient.Get(creditsURL)
	if err != nil {
		return nil, fmt.Errorf("tmdb fetch credits: %w", err)
	}
	defer cResp.Body.Close() //nolint:errcheck
	cBody, err := io.ReadAll(cResp.Body)
	if err != nil {
		return nil, err
	}
	var cData map[string]any
	if err := json.Unmarshal(cBody, &cData); err != nil {
		return nil, err
	}

	// Build structured result
	result := map[string]any{
		"tmdb_id":    tmdbID,
		"title":      stringFromMap(mData, "title"),
		"sort_title": stringFromMap(mData, "original_title"),
		"year":       releaseYear(stringFromMap(mData, "release_date")),
		"plot":       stringFromMap(mData, "overview"),
		"tagline":    stringFromMap(mData, "tagline"),
		"runtime":    intFromMap(mData, "runtime"),
		"rating":     floatFromMap(mData, "vote_average"),
		"imdb_id":    stringFromMap(mData, "imdb_id"),
		"poster_url": tmdbImageFromMap(mData, "poster_path"),
		"fanart_url": tmdbImageFromMap(mData, "backdrop_path"),
	}

	// Tags from genres
	var tags []string
	if genres, ok := mData["genres"].([]any); ok {
		for _, g := range genres {
			gm, ok := g.(map[string]any)
			if !ok {
				continue
			}
			if name, ok := gm["name"].(string); ok && name != "" {
				tags = append(tags, strings.ToLower(name))
			}
		}
	}
	result["tags"] = tags

	// Actors from cast (top 10)
	var actors []map[string]any
	if cast, ok := cData["cast"].([]any); ok {
		for i, c := range cast {
			if i >= 10 {
				break
			}
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			actors = append(actors, map[string]any{
				"name":       stringFromMap(cm, "name"),
				"role":       stringFromMap(cm, "character"),
				"sort_order": i,
			})
		}
	}
	result["actors"] = actors

	return result, nil
}

func (svc *Service) actionListMoviesByActor(params map[string]any) (any, error) {
	actor, _ := params["actor"].(string)
	if actor == "" {
		return map[string]any{"movies": []any{}}, nil
	}
	sort, _ := params["sort"].(string)
	movies, err := listMoviesByActor(svc.db, actor, sort)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, len(movies))
	for i, m := range movies {
		result[i] = movieToMap(m)
	}
	return map[string]any{"movies": result}, nil
}

func (svc *Service) actionGetTagCounts(params map[string]any) (any, error) {
	search, _ := params["search"].(string)
	fulltext, _ := params["fulltext"].(bool)
	return getTagCounts(svc.db, search, fulltext)
}

// movieFromParams builds a movie struct from action params.
func movieFromParams(params map[string]any) movie {
	m := movie{
		Title:      stringParam(params, "title"),
		SortTitle:  stringParam(params, "sort_title"),
		Year:       intParam(params, "year"),
		Plot:       stringParam(params, "plot"),
		Tagline:    stringParam(params, "tagline"),
		Runtime:    intParam(params, "runtime"),
		Rating:     floatParam(params, "rating"),
		TmdbID:     stringParam(params, "tmdb_id"),
		ImdbID:     stringParam(params, "imdb_id"),
		PosterURL:  stripVersionParam(stringParam(params, "poster_url")),
		FanartURL:  stripVersionParam(stringParam(params, "fanart_url")),
		SetName:    stringParam(params, "set_name"),
		LastPlayed: stringParam(params, "last_played"),
	}

	// Tags
	if rawTags, ok := params["tags"].([]any); ok {
		for _, t := range rawTags {
			if s, ok := t.(string); ok && strings.TrimSpace(s) != "" {
				m.Tags = append(m.Tags, strings.ToLower(strings.TrimSpace(s)))
			}
		}
	}
	if m.Tags == nil {
		m.Tags = []string{}
	}

	// Actors
	if rawActors, ok := params["actors"].([]any); ok {
		for i, a := range rawActors {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			name := stringParam(am, "name")
			if name == "" {
				continue
			}
			order := intParam(am, "order")
			if order == 0 {
				order = i
			}
			m.Actors = append(m.Actors, movieActor{
				Name:      name,
				Role:      stringParam(am, "role"),
				SortOrder: order,
			})
		}
	}
	if m.Actors == nil {
		m.Actors = []movieActor{}
	}

	return m
}

func stringParam(params map[string]any, key string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	return ""
}

func intParam(params map[string]any, key string) int {
	switch v := params[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func floatParam(params map[string]any, key string) float64 {
	if v, ok := params[key].(float64); ok {
		return v
	}
	return 0
}

func stringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func intFromMap(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func floatFromMap(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func tmdbImageFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return tmdbImageBase + v
	}
	return ""
}

func releaseYear(dateStr string) int {
	if len(dateStr) >= 4 {
		year := 0
		for _, c := range dateStr[:4] {
			if c >= '0' && c <= '9' {
				year = year*10 + int(c-'0')
			} else {
				return 0
			}
		}
		return year
	}
	return 0
}
