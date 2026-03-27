package movies

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"keyop/core"
)

// newTestMoviesService creates a fully-initialised Service backed by a temp DB.
// The Deps logger is set so that paths calling MustGetLogger() don't panic.
func newTestMoviesService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	svc := &Service{
		Deps:       deps,
		Cfg:        core.ServiceConfig{Name: "movies"},
		dbPath:     filepath.Join(dir, "movies.sql"),
		imagesDir:  filepath.Join(dir, "images"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	if err := svc.Initialize(); err != nil {
		t.Fatalf("newTestMoviesService: Initialize: %v", err)
	}
	return svc
}

// mockTMDBTransport rewrites requests aimed at api.themoviedb.org / image.tmdb.org
// to a local httptest.Server instead.
type mockTMDBTransport struct {
	serverURL string
}

func (tr *mockTMDBTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	parsed, _ := url.Parse(tr.serverURL)
	req2.URL.Scheme = parsed.Scheme
	req2.URL.Host = parsed.Host
	return http.DefaultTransport.RoundTrip(req2) //nolint:noctx
}

// ── Initialize / Check ────────────────────────────────────────────────────────

func TestInitialize_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})
	svc := &Service{
		Deps:      deps,
		dbPath:    filepath.Join(dir, "sub", "movies.sql"),
		imagesDir: filepath.Join(dir, "img", "movies"),
	}
	if err := svc.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	for _, d := range []string{
		filepath.Join(dir, "sub"),
		filepath.Join(dir, "img", "movies"),
	} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Errorf("expected dir %s to exist after Initialize, got: %v", d, err)
			continue
		}
		if !fi.IsDir() {
			t.Errorf("%s should be a directory", d)
		}
	}
}

func TestCheck_AfterInitialize(t *testing.T) {
	svc := newTestMoviesService(t)
	if err := svc.Check(); err != nil {
		t.Errorf("Check() after Initialize should return nil, got: %v", err)
	}
}

func TestCheck_BeforeInitialize(t *testing.T) {
	svc := &Service{}
	if err := svc.Check(); err == nil {
		t.Error("Check() before Initialize should return an error")
	}
}

// ── stringFromMap / intFromMap / floatFromMap / tmdbImageFromMap ──────────────

func TestStringFromMap(t *testing.T) {
	cases := []struct {
		m    map[string]any
		key  string
		want string
	}{
		{map[string]any{"k": "hello"}, "k", "hello"},
		{map[string]any{"k": ""}, "k", ""},
		{map[string]any{"k": 42}, "k", ""},   // wrong type
		{map[string]any{}, "k", ""},          // missing key
		{map[string]any{"k": nil}, "k", ""},  // nil value
		{map[string]any{"k": true}, "k", ""}, // bool, not string
	}
	for _, c := range cases {
		if got := stringFromMap(c.m, c.key); got != c.want {
			t.Errorf("stringFromMap(%v, %q) = %q, want %q", c.m, c.key, got, c.want)
		}
	}
}

func TestIntFromMap(t *testing.T) {
	cases := []struct {
		m    map[string]any
		key  string
		want int
	}{
		{map[string]any{"k": float64(42)}, "k", 42},
		{map[string]any{"k": float64(0)}, "k", 0},
		{map[string]any{"k": int(7)}, "k", 7},
		{map[string]any{"k": "hello"}, "k", 0}, // string → 0
		{map[string]any{}, "k", 0},             // missing → 0
		{map[string]any{"k": nil}, "k", 0},     // nil → 0
		{map[string]any{"k": float64(120)}, "k", 120},
	}
	for _, c := range cases {
		if got := intFromMap(c.m, c.key); got != c.want {
			t.Errorf("intFromMap(%v, %q) = %d, want %d", c.m, c.key, got, c.want)
		}
	}
}

func TestFloatFromMap(t *testing.T) {
	cases := []struct {
		m    map[string]any
		key  string
		want float64
	}{
		{map[string]any{"k": float64(7.5)}, "k", 7.5},
		{map[string]any{"k": float64(0)}, "k", 0},
		{map[string]any{"k": "x"}, "k", 0},    // wrong type
		{map[string]any{}, "k", 0},            // missing
		{map[string]any{"k": nil}, "k", 0},    // nil
		{map[string]any{"k": int(3)}, "k", 0}, // int (not float64) → 0
	}
	for _, c := range cases {
		if got := floatFromMap(c.m, c.key); got != c.want {
			t.Errorf("floatFromMap(%v, %q) = %f, want %f", c.m, c.key, got, c.want)
		}
	}
}

func TestTmdbImageFromMap(t *testing.T) {
	cases := []struct {
		m    map[string]any
		key  string
		want string
	}{
		{map[string]any{"k": "/abc.jpg"}, "k", tmdbImageBase + "/abc.jpg"},
		{map[string]any{"k": ""}, "k", ""},  // empty string → no URL
		{map[string]any{}, "k", ""},         // missing key
		{map[string]any{"k": nil}, "k", ""}, // nil
		{map[string]any{"k": 42}, "k", ""},  // wrong type
	}
	for _, c := range cases {
		if got := tmdbImageFromMap(c.m, c.key); got != c.want {
			t.Errorf("tmdbImageFromMap(%v, %q) = %q, want %q", c.m, c.key, got, c.want)
		}
	}
}

// ── actionImportNFO ────────────────────────────────────────────────────────────

const minimalNFO = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>Imported Movie</title>
  <year>2021</year>
  <plot>A test plot.</plot>
</movie>`

func TestActionImportNFO_Basic(t *testing.T) {
	svc := newTestMoviesService(t)
	params := map[string]any{
		"files": []any{
			map[string]any{
				"name":    "imported.nfo",
				"content": minimalNFO,
			},
		},
	}

	result, err := svc.HandleWebUIAction("import-nfo", params)
	if err != nil {
		t.Fatalf("import-nfo: %v", err)
	}

	rm, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", result)
	}

	imported, _ := rm["imported"].([]map[string]any)
	if len(imported) != 1 {
		t.Fatalf("expected 1 imported movie, got %d", len(imported))
	}
	if imported[0]["title"] != "Imported Movie" {
		t.Errorf("title: want %q, got %v", "Imported Movie", imported[0]["title"])
	}
	if imported[0]["year"] != 2021 {
		t.Errorf("year: want 2021, got %v", imported[0]["year"])
	}
	if rm["skipped"] != 0 {
		t.Errorf("skipped: want 0, got %v", rm["skipped"])
	}
	errors, _ := rm["errors"].([]string)
	if len(errors) != 0 {
		t.Errorf("unexpected errors: %v", errors)
	}
}

func TestActionImportNFO_Duplicate(t *testing.T) {
	svc := newTestMoviesService(t)
	params := map[string]any{
		"files": []any{
			map[string]any{"name": "a.nfo", "content": minimalNFO},
		},
	}

	// First import.
	if _, err := svc.HandleWebUIAction("import-nfo", params); err != nil {
		t.Fatalf("first import: %v", err)
	}

	// Second import of the same title+year should be skipped.
	result, err := svc.HandleWebUIAction("import-nfo", params)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	rm := result.(map[string]any)
	if rm["skipped"] != 1 {
		t.Errorf("skipped: want 1, got %v", rm["skipped"])
	}
	imported, _ := rm["imported"].([]map[string]any)
	if len(imported) != 0 {
		t.Errorf("expected no newly imported movies, got %d", len(imported))
	}
}

func TestActionImportNFO_InvalidXML(t *testing.T) {
	svc := newTestMoviesService(t)
	params := map[string]any{
		"files": []any{
			map[string]any{"name": "bad.nfo", "content": "this is not xml <<<"},
		},
	}

	result, err := svc.HandleWebUIAction("import-nfo", params)
	if err != nil {
		t.Fatalf("import-nfo with bad xml should not return a top-level error: %v", err)
	}
	rm := result.(map[string]any)
	errors, _ := rm["errors"].([]string)
	if len(errors) != 1 {
		t.Errorf("expected 1 error for bad XML, got %d: %v", len(errors), errors)
	}
	imported, _ := rm["imported"].([]map[string]any)
	if len(imported) != 0 {
		t.Errorf("expected 0 imported on parse error, got %d", len(imported))
	}
}

func TestActionImportNFO_EmptyFiles(t *testing.T) {
	svc := newTestMoviesService(t)
	params := map[string]any{
		"files": []any{},
	}
	result, err := svc.HandleWebUIAction("import-nfo", params)
	if err != nil {
		t.Fatalf("import-nfo with empty files: %v", err)
	}
	rm := result.(map[string]any)
	imported, _ := rm["imported"].([]map[string]any)
	if len(imported) != 0 {
		t.Errorf("expected 0 imported, got %d", len(imported))
	}
}

// ── actionListMoviesByActor ───────────────────────────────────────────────────

func TestActionListMoviesByActor(t *testing.T) {
	svc := newTestMoviesService(t)

	// Seed two movies with a shared actor plus one unrelated movie.
	mustInsert(t, svc.db, movie{
		Title:  "Alien",
		Actors: []movieActor{{Name: "Sigourney Weaver", SortOrder: 0}},
	})
	mustInsert(t, svc.db, movie{
		Title:  "Avatar",
		Actors: []movieActor{{Name: "Sigourney Weaver", SortOrder: 0}},
	})
	mustInsert(t, svc.db, movie{
		Title:  "Batman",
		Actors: []movieActor{{Name: "Michael Keaton", SortOrder: 0}},
	})

	result, err := svc.HandleWebUIAction("list-movies-by-actor", map[string]any{
		"actor": "Sigourney Weaver",
		"sort":  "title",
	})
	if err != nil {
		t.Fatalf("list-movies-by-actor: %v", err)
	}
	rm := result.(map[string]any)
	movies, _ := rm["movies"].([]map[string]any)
	if len(movies) != 2 {
		t.Errorf("expected 2 movies for Sigourney Weaver, got %d", len(movies))
	}
}

func TestActionListMoviesByActor_EmptyActor(t *testing.T) {
	svc := newTestMoviesService(t)
	result, err := svc.HandleWebUIAction("list-movies-by-actor", map[string]any{
		"actor": "",
	})
	if err != nil {
		t.Fatalf("list-movies-by-actor empty actor: %v", err)
	}
	rm := result.(map[string]any)
	movies, _ := rm["movies"].([]any)
	if len(movies) != 0 {
		t.Errorf("expected 0 movies for empty actor, got %d", len(movies))
	}
}

// ── actionGetTagCounts ────────────────────────────────────────────────────────

func TestActionGetTagCounts(t *testing.T) {
	svc := newTestMoviesService(t)

	mustInsert(t, svc.db, movie{Title: "A", Tags: []string{"horror", "sci-fi"}})
	mustInsert(t, svc.db, movie{Title: "B", Tags: []string{"horror"}})
	mustInsert(t, svc.db, movie{Title: "C", Tags: []string{"comedy"}})

	result, err := svc.HandleWebUIAction("get-tag-counts", map[string]any{})
	if err != nil {
		t.Fatalf("get-tag-counts: %v", err)
	}
	rm := result.(map[string]any)
	total, _ := rm["total"].(int)
	if total != 3 {
		t.Errorf("total: want 3, got %d", total)
	}
	counts, _ := rm["counts"].([]map[string]any)
	if len(counts) != 3 {
		t.Errorf("expected 3 distinct tags, got %d", len(counts))
	}
	// horror appears in 2 movies — should be first
	if len(counts) > 0 && counts[0]["tag"] != "horror" {
		t.Errorf("horror should have highest count, got %v", counts[0]["tag"])
	}
}

func TestActionGetTagCounts_Empty(t *testing.T) {
	svc := newTestMoviesService(t)
	result, err := svc.HandleWebUIAction("get-tag-counts", map[string]any{})
	if err != nil {
		t.Fatalf("get-tag-counts on empty db: %v", err)
	}
	rm := result.(map[string]any)
	counts, _ := rm["counts"].([]map[string]any)
	if len(counts) != 0 {
		t.Errorf("expected 0 tag counts on empty db, got %d", len(counts))
	}
}

// ── actionSearchTMDB ──────────────────────────────────────────────────────────

func TestActionSearchTMDB_NoAPIKey(t *testing.T) {
	svc := newTestMoviesService(t)
	// tmdbAPIKey is empty by default.

	result, err := svc.HandleWebUIAction("search-tmdb", map[string]any{"query": "alien"})
	if err != nil {
		t.Fatalf("search-tmdb (no key) returned error: %v", err)
	}
	rm, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if rm["error"] == nil {
		t.Errorf("expected error field when tmdb_api_key not configured, got: %v", rm)
	}
}

func TestActionSearchTMDB_Success(t *testing.T) {
	// Build a minimal TMDB search response.
	tmdbResp := map[string]any{
		"results": []any{
			map[string]any{
				"id":          float64(348),
				"title":       "Alien",
				"poster_path": "/poster.jpg",
			},
		},
	}
	body, _ := json.Marshal(tmdbResp)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer ts.Close()

	svc := newTestMoviesService(t)
	svc.tmdbAPIKey = "test-key"
	svc.httpClient = &http.Client{
		Transport: &mockTMDBTransport{serverURL: ts.URL},
	}

	result, err := svc.HandleWebUIAction("search-tmdb", map[string]any{"query": "alien"})
	if err != nil {
		t.Fatalf("search-tmdb: %v", err)
	}
	rm := result.(map[string]any)
	results, _ := rm["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	// poster_url should have been built from poster_path
	item := results[0].(map[string]any)
	wantPosterURL := tmdbImageBase + "/poster.jpg"
	if item["poster_url"] != wantPosterURL {
		t.Errorf("poster_url: want %q, got %v", wantPosterURL, item["poster_url"])
	}
}

// ── actionFetchTMDB ───────────────────────────────────────────────────────────

func TestActionFetchTMDB_NoAPIKey(t *testing.T) {
	svc := newTestMoviesService(t)

	result, err := svc.HandleWebUIAction("fetch-tmdb", map[string]any{"tmdb_id": "348"})
	if err != nil {
		t.Fatalf("fetch-tmdb (no key) returned error: %v", err)
	}
	rm, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if rm["error"] == nil {
		t.Errorf("expected error field when tmdb_api_key not configured, got: %v", rm)
	}
}

func TestActionFetchTMDB_MissingTmdbID(t *testing.T) {
	svc := newTestMoviesService(t)
	svc.tmdbAPIKey = "test-key"

	_, err := svc.HandleWebUIAction("fetch-tmdb", map[string]any{})
	if err == nil {
		t.Error("expected error when tmdb_id is missing")
	}
}

func TestActionFetchTMDB_Success(t *testing.T) {
	movieData := map[string]any{
		"title":          "Alien",
		"original_title": "Alien",
		"release_date":   "1979-05-25",
		"overview":       "A terrifying film.",
		"tagline":        "In space no one can hear you scream.",
		"runtime":        float64(117),
		"vote_average":   float64(8.4),
		"imdb_id":        "tt0078748",
		"poster_path":    "/poster.jpg",
		"backdrop_path":  "/fanart.jpg",
		"genres": []any{
			map[string]any{"id": float64(27), "name": "Horror"},
			map[string]any{"id": float64(878), "name": "Science Fiction"},
		},
	}
	creditsData := map[string]any{
		"cast": []any{
			map[string]any{"name": "Sigourney Weaver", "character": "Ripley"},
			map[string]any{"name": "Tom Skerritt", "character": "Dallas"},
		},
	}

	movieBody, _ := json.Marshal(movieData)
	creditsBody, _ := json.Marshal(creditsData)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if len(r.URL.Path) > 0 && r.URL.Path[len(r.URL.Path)-len("credits"):] == "credits" {
			_, _ = w.Write(creditsBody)
		} else {
			_, _ = w.Write(movieBody)
		}
	}))
	defer ts.Close()

	svc := newTestMoviesService(t)
	svc.tmdbAPIKey = "test-key"
	svc.httpClient = &http.Client{
		Transport: &mockTMDBTransport{serverURL: ts.URL},
	}

	result, err := svc.HandleWebUIAction("fetch-tmdb", map[string]any{"tmdb_id": "348"})
	if err != nil {
		t.Fatalf("fetch-tmdb: %v", err)
	}
	rm := result.(map[string]any)

	if rm["title"] != "Alien" {
		t.Errorf("title: want %q, got %v", "Alien", rm["title"])
	}
	if rm["year"] != 1979 {
		t.Errorf("year: want 1979, got %v", rm["year"])
	}
	if rm["imdb_id"] != "tt0078748" {
		t.Errorf("imdb_id: want %q, got %v", "tt0078748", rm["imdb_id"])
	}
	if rm["poster_url"] != tmdbImageBase+"/poster.jpg" {
		t.Errorf("poster_url: want %q, got %v", tmdbImageBase+"/poster.jpg", rm["poster_url"])
	}
	if rm["fanart_url"] != tmdbImageBase+"/fanart.jpg" {
		t.Errorf("fanart_url: want %q, got %v", tmdbImageBase+"/fanart.jpg", rm["fanart_url"])
	}

	tags, _ := rm["tags"].([]string)
	if len(tags) != 2 {
		t.Errorf("expected 2 genre tags, got %d: %v", len(tags), tags)
	}

	actors, _ := rm["actors"].([]map[string]any)
	if len(actors) != 2 {
		t.Fatalf("expected 2 actors, got %d", len(actors))
	}
	if actors[0]["name"] != "Sigourney Weaver" {
		t.Errorf("actor[0].name: want %q, got %v", "Sigourney Weaver", actors[0]["name"])
	}
	if actors[0]["role"] != "Ripley" {
		t.Errorf("actor[0].role: want %q, got %v", "Ripley", actors[0]["role"])
	}
}

func TestActionFetchTMDB_LimitsActorsToTen(t *testing.T) {
	// Build a cast with 15 entries — result should be trimmed to 10.
	cast := make([]any, 15)
	for i := range cast {
		cast[i] = map[string]any{"name": "Actor", "character": "Role"}
	}
	movieBody, _ := json.Marshal(map[string]any{
		"title": "Big Cast", "release_date": "2000-01-01",
	})
	creditsBody, _ := json.Marshal(map[string]any{"cast": cast})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if len(r.URL.Path) > 0 && r.URL.Path[len(r.URL.Path)-len("credits"):] == "credits" {
			_, _ = w.Write(creditsBody)
		} else {
			_, _ = w.Write(movieBody)
		}
	}))
	defer ts.Close()

	svc := newTestMoviesService(t)
	svc.tmdbAPIKey = "test-key"
	svc.httpClient = &http.Client{
		Transport: &mockTMDBTransport{serverURL: ts.URL},
	}

	result, err := svc.HandleWebUIAction("fetch-tmdb", map[string]any{"tmdb_id": "999"})
	if err != nil {
		t.Fatalf("fetch-tmdb: %v", err)
	}
	actors, _ := result.(map[string]any)["actors"].([]map[string]any)
	if len(actors) != 10 {
		t.Errorf("expected actors capped at 10, got %d", len(actors))
	}
}

// ── HandleWebUIAction unknown action ─────────────────────────────────────────

func TestHandleWebUIAction_Unknown(t *testing.T) {
	svc := newTestMoviesService(t)
	_, err := svc.HandleWebUIAction("no-such-action", map[string]any{})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}
