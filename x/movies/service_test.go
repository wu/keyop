package movies

import (
	"fmt"
	"testing"
	"time"
)

// ── releaseYear ───────────────────────────────────────────────────────────────

func TestReleaseYear_Valid(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"2001-01-01", 2001},
		{"1979", 1979},
		{"2024-12-31", 2024},
		{"1900-06-15", 1900},
	}
	for _, c := range cases {
		if got := releaseYear(c.in); got != c.want {
			t.Errorf("releaseYear(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestReleaseYear_Invalid(t *testing.T) {
	cases := []string{"", "abc", "20x4-01-01", "   "}
	for _, c := range cases {
		if got := releaseYear(c); got != 0 {
			t.Errorf("releaseYear(%q) = %d, want 0", c, got)
		}
	}
}

func TestReleaseYear_ShortString(t *testing.T) {
	if got := releaseYear("200"); got != 0 {
		t.Errorf("releaseYear(\"200\") = %d, want 0", got)
	}
}

// ── imageExtFromContentType ───────────────────────────────────────────────────

func TestImageExtFromContentType(t *testing.T) {
	cases := []struct {
		ct   string
		want string
	}{
		{"image/jpeg", ".jpg"},
		{"image/jpg", ".jpg"},
		{"image/png", ".png"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"image/jpeg; charset=utf-8", ".jpg"}, // strips parameters
		{"IMAGE/JPEG", ".jpg"},                // case-insensitive
		{"", ""},                              // unknown → empty
		{"text/html", ""},                     // unknown → empty
		{"image/avif", ""},                    // unsupported → empty
	}
	for _, c := range cases {
		if got := imageExtFromContentType(c.ct); got != c.want {
			t.Errorf("imageExtFromContentType(%q) = %q, want %q", c.ct, got, c.want)
		}
	}
}

// ── expandHome ────────────────────────────────────────────────────────────────

func TestExpandHome_Tilde(t *testing.T) {
	got := expandHome("~/foo/bar")
	if got == "~/foo/bar" {
		t.Error("tilde was not expanded")
	}
	if len(got) < 4 || got[len(got)-4:] != "/bar" {
		t.Errorf("unexpected expanded path: %q", got)
	}
}

func TestExpandHome_NoTilde(t *testing.T) {
	got := expandHome("/absolute/path")
	if got != "/absolute/path" {
		t.Errorf("expandHome should not modify non-tilde paths, got %q", got)
	}
}

func TestExpandHome_Empty(t *testing.T) {
	if got := expandHome(""); got != "" {
		t.Errorf("expandHome(\"\") = %q, want \"\"", got)
	}
}

// ── stringParam / intParam / floatParam ───────────────────────────────────────

func TestStringParam(t *testing.T) {
	p := map[string]any{"name": "Alien", "count": 42}
	if got := stringParam(p, "name"); got != "Alien" {
		t.Errorf("stringParam string: %q", got)
	}
	if got := stringParam(p, "count"); got != "" {
		t.Errorf("stringParam non-string should return \"\", got %q", got)
	}
	if got := stringParam(p, "missing"); got != "" {
		t.Errorf("stringParam missing key: %q", got)
	}
}

func TestIntParam(t *testing.T) {
	p := map[string]any{"n": float64(42), "s": "hello", "i": 7}
	if got := intParam(p, "n"); got != 42 {
		t.Errorf("intParam float64: %d", got)
	}
	if got := intParam(p, "i"); got != 7 {
		t.Errorf("intParam int: %d", got)
	}
	if got := intParam(p, "s"); got != 0 {
		t.Errorf("intParam non-numeric: %d", got)
	}
	if got := intParam(p, "missing"); got != 0 {
		t.Errorf("intParam missing: %d", got)
	}
}

func TestFloatParam(t *testing.T) {
	p := map[string]any{"f": float64(3.14), "s": "x"}
	if got := floatParam(p, "f"); got != 3.14 {
		t.Errorf("floatParam: %f", got)
	}
	if got := floatParam(p, "s"); got != 0 {
		t.Errorf("floatParam non-float: %f", got)
	}
}

// ── movieToMap ────────────────────────────────────────────────────────────────

func TestMovieToMap_Fields(t *testing.T) {
	updatedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := movie{
		ID:         42,
		UUID:       "abc-123",
		Title:      "Alien",
		SortTitle:  "Alien",
		Year:       1979,
		Plot:       "Scary stuff",
		Tagline:    "In space...",
		Runtime:    117,
		Rating:     8.5,
		TmdbID:     "348",
		ImdbID:     "tt0078748",
		PosterURL:  "/img/poster.jpg",
		FanartURL:  "/img/fanart.jpg",
		SetName:    "Alien Collection",
		LastPlayed: "2026-01-01",
		Tags:       []string{"horror"},
		Actors:     []movieActor{{Name: "Sigourney Weaver", Role: "Ripley", SortOrder: 0}},
		UpdatedAt:  updatedAt,
	}

	result := movieToMap(m)

	check := func(key string, want any) {
		t.Helper()
		if result[key] != want {
			t.Errorf("movieToMap[%q] = %v, want %v", key, result[key], want)
		}
	}

	check("id", int64(42))
	check("uuid", "abc-123")
	check("title", "Alien")
	check("year", 1979)
	check("runtime", 117)
	check("rating", 8.5)
	check("tmdb_id", "348")
	check("imdb_id", "tt0078748")
	check("poster_url", fmt.Sprintf("/img/poster.jpg?v=%d", updatedAt.Unix()))
	check("set_name", "Alien Collection")
	check("last_played", "2026-01-01")

	tags, ok := result["tags"].([]string)
	if !ok || len(tags) != 1 || tags[0] != "horror" {
		t.Errorf("tags: %v", result["tags"])
	}

	actors, ok := result["actors"].([]map[string]any)
	if !ok || len(actors) != 1 {
		t.Errorf("actors: %v", result["actors"])
	}
	if actors[0]["name"] != "Sigourney Weaver" {
		t.Errorf("actor name: %v", actors[0]["name"])
	}
}

func TestMovieToMap_EmptyTagsAndActors(t *testing.T) {
	m := movie{Title: "Minimal", Tags: []string{}, Actors: []movieActor{}}
	result := movieToMap(m)

	tags, ok := result["tags"].([]string)
	if !ok {
		t.Errorf("tags should be []string, got %T", result["tags"])
	}
	if len(tags) != 0 {
		t.Errorf("expected empty tags, got %v", tags)
	}
}

// ── movieFromParams ───────────────────────────────────────────────────────────

func TestMovieFromParams_BasicFields(t *testing.T) {
	params := map[string]any{
		"title":       "Alien",
		"sort_title":  "Alien",
		"year":        float64(1979),
		"plot":        "Scary",
		"tagline":     "In space...",
		"runtime":     float64(117),
		"rating":      8.5,
		"tmdb_id":     "348",
		"imdb_id":     "tt0078748",
		"poster_url":  "/img/poster.jpg",
		"fanart_url":  "/img/fanart.jpg",
		"set_name":    "Alien Collection",
		"last_played": "2026-01-01",
	}

	m := movieFromParams(params)

	if m.Title != "Alien" {
		t.Errorf("title: %q", m.Title)
	}
	if m.Year != 1979 {
		t.Errorf("year: %d", m.Year)
	}
	if m.Runtime != 117 {
		t.Errorf("runtime: %d", m.Runtime)
	}
	if m.Rating != 8.5 {
		t.Errorf("rating: %f", m.Rating)
	}
	if m.SetName != "Alien Collection" {
		t.Errorf("set_name: %q", m.SetName)
	}
	if m.LastPlayed != "2026-01-01" {
		t.Errorf("last_played: %q", m.LastPlayed)
	}
}

func TestMovieFromParams_Tags(t *testing.T) {
	params := map[string]any{
		"title": "T",
		"tags":  []any{"horror", "sci-fi"},
	}
	m := movieFromParams(params)
	if len(m.Tags) != 2 {
		t.Errorf("tags: %v", m.Tags)
	}
}

func TestMovieFromParams_Actors(t *testing.T) {
	params := map[string]any{
		"title": "T",
		"actors": []any{
			map[string]any{"name": "Actor A", "role": "Hero", "sort_order": float64(0)},
			map[string]any{"name": "Actor B", "role": "Villain", "sort_order": float64(1)},
		},
	}
	m := movieFromParams(params)
	if len(m.Actors) != 2 {
		t.Fatalf("expected 2 actors, got %d", len(m.Actors))
	}
	if m.Actors[0].Name != "Actor A" || m.Actors[0].Role != "Hero" {
		t.Errorf("actor[0]: %+v", m.Actors[0])
	}
}

func TestMovieFromParams_EmptyParams(t *testing.T) {
	m := movieFromParams(map[string]any{})
	if m.Title != "" {
		t.Errorf("empty params should give empty title, got %q", m.Title)
	}
	if m.Year != 0 {
		t.Errorf("empty params should give year 0, got %d", m.Year)
	}
}
