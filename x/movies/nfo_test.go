package movies

import (
	"strings"
	"testing"
)

const nfoFull = `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<movie>
  <title>Alien</title>
  <sorttitle>Alien</sorttitle>
  <year>1979</year>
  <plot>A crew encounters a deadly alien.</plot>
  <tagline>In space no one can hear you scream.</tagline>
  <runtime>117</runtime>
  <rating>8.5</rating>
  <uniqueid type="tmdb" default="true">348</uniqueid>
  <uniqueid type="imdb">tt0078748</uniqueid>
  <genre>Horror</genre>
  <genre>Sci-Fi</genre>
  <tag>classic</tag>
  <thumb aspect="poster">https://example.com/poster.jpg</thumb>
  <thumb aspect="banner">https://example.com/banner.jpg</thumb>
  <fanart>
    <thumb>https://example.com/fanart.jpg</thumb>
    <thumb>https://example.com/fanart2.jpg</thumb>
  </fanart>
  <actor>
    <name>Sigourney Weaver</name>
    <role>Ellen Ripley</role>
    <order>0</order>
  </actor>
  <actor>
    <name>Tom Skerritt</name>
    <role>Dallas</role>
    <order>1</order>
  </actor>
  <lastplayed>1979-05-25</lastplayed>
  <set>
    <name>Alien Collection</name>
  </set>
</movie>`

func TestParseNFO_Title(t *testing.T) {
	m, err := parseNFO(nfoFull)
	if err != nil {
		t.Fatalf("parseNFO: %v", err)
	}
	if m.Title != "Alien" {
		t.Errorf("title: want %q, got %q", "Alien", m.Title)
	}
}

func TestParseNFO_BasicFields(t *testing.T) {
	m, _ := parseNFO(nfoFull)
	if m.Year != 1979 {
		t.Errorf("year: want 1979, got %d", m.Year)
	}
	if m.Runtime != 117 {
		t.Errorf("runtime: want 117, got %d", m.Runtime)
	}
	if m.Rating != 8.5 {
		t.Errorf("rating: want 8.5, got %f", m.Rating)
	}
	if m.Plot != "A crew encounters a deadly alien." {
		t.Errorf("plot: %q", m.Plot)
	}
	if m.Tagline != "In space no one can hear you scream." {
		t.Errorf("tagline: %q", m.Tagline)
	}
}

func TestParseNFO_IDs(t *testing.T) {
	m, _ := parseNFO(nfoFull)
	if m.TmdbID != "348" {
		t.Errorf("tmdb_id: want %q, got %q", "348", m.TmdbID)
	}
	if m.ImdbID != "tt0078748" {
		t.Errorf("imdb_id: want %q, got %q", "tt0078748", m.ImdbID)
	}
}

func TestParseNFO_GenresAndTagsMergedLowercase(t *testing.T) {
	m, _ := parseNFO(nfoFull)
	// 2 genres (horror, sci-fi) + 1 tag (classic) = 3 total, all lowercase
	if len(m.Tags) != 3 {
		t.Errorf("expected 3 tags, got %d: %v", len(m.Tags), m.Tags)
	}
	for _, tag := range m.Tags {
		if tag != strings.ToLower(tag) {
			t.Errorf("tag not lowercase: %q", tag)
		}
	}
}

func TestParseNFO_GenreDeduplication(t *testing.T) {
	nfo := `<movie>
		<title>T</title>
		<genre>Horror</genre>
		<genre>horror</genre>
		<tag>Horror</tag>
	</movie>`
	m, _ := parseNFO(nfo)
	if len(m.Tags) != 1 {
		t.Errorf("duplicate genres/tags not deduped: %v", m.Tags)
	}
}

func TestParseNFO_PosterURL_AspectPoster(t *testing.T) {
	m, _ := parseNFO(nfoFull)
	if m.PosterURL != "https://example.com/poster.jpg" {
		t.Errorf("poster_url: %q", m.PosterURL)
	}
}

func TestParseNFO_PosterURL_IgnoresBanner(t *testing.T) {
	// Only thumb with aspect="poster" should be picked
	nfo := `<movie>
		<title>T</title>
		<thumb aspect="banner">https://example.com/banner.jpg</thumb>
		<thumb aspect="poster">https://example.com/poster.jpg</thumb>
	</movie>`
	m, _ := parseNFO(nfo)
	if m.PosterURL != "https://example.com/poster.jpg" {
		t.Errorf("poster_url should be poster aspect, got %q", m.PosterURL)
	}
}

func TestParseNFO_FanartURL_FirstThumb(t *testing.T) {
	m, _ := parseNFO(nfoFull)
	if m.FanartURL != "https://example.com/fanart.jpg" {
		t.Errorf("fanart_url: %q", m.FanartURL)
	}
}

func TestParseNFO_Actors(t *testing.T) {
	m, _ := parseNFO(nfoFull)
	if len(m.Actors) != 2 {
		t.Fatalf("expected 2 actors, got %d", len(m.Actors))
	}
	if m.Actors[0].Name != "Sigourney Weaver" {
		t.Errorf("actor[0].name: %q", m.Actors[0].Name)
	}
	if m.Actors[0].Role != "Ellen Ripley" {
		t.Errorf("actor[0].role: %q", m.Actors[0].Role)
	}
	if m.Actors[0].SortOrder != 0 {
		t.Errorf("actor[0].sort_order: %d", m.Actors[0].SortOrder)
	}
}

func TestParseNFO_ActorSortOrderFallsBackToIndex(t *testing.T) {
	// When <order> is 0 for the second actor, index is used as fallback
	nfo := `<movie>
		<title>T</title>
		<actor><name>First Actor</name><order>0</order></actor>
		<actor><name>Second Actor</name></actor>
	</movie>`
	m, _ := parseNFO(nfo)
	if len(m.Actors) != 2 {
		t.Fatalf("expected 2 actors, got %d", len(m.Actors))
	}
	if m.Actors[1].SortOrder != 1 {
		t.Errorf("second actor sort_order should default to index 1, got %d", m.Actors[1].SortOrder)
	}
}

func TestParseNFO_SkipsBlankActorNames(t *testing.T) {
	nfo := `<movie>
		<title>T</title>
		<actor><name>  </name></actor>
		<actor><name>Real Actor</name></actor>
	</movie>`
	m, _ := parseNFO(nfo)
	if len(m.Actors) != 1 {
		t.Errorf("blank actor name should be skipped, got %d actors", len(m.Actors))
	}
}

func TestParseNFO_LastPlayed(t *testing.T) {
	m, _ := parseNFO(nfoFull)
	if m.LastPlayed != "1979-05-25" {
		t.Errorf("last_played: %q", m.LastPlayed)
	}
}

func TestParseNFO_SetName(t *testing.T) {
	m, _ := parseNFO(nfoFull)
	if m.SetName != "Alien Collection" {
		t.Errorf("set_name: %q", m.SetName)
	}
}

func TestParseNFO_MinimalXML(t *testing.T) {
	nfo := `<movie><title>Minimal</title></movie>`
	m, err := parseNFO(nfo)
	if err != nil {
		t.Fatalf("minimal NFO: %v", err)
	}
	if m.Title != "Minimal" {
		t.Errorf("title: %q", m.Title)
	}
	if len(m.Tags) != 0 {
		t.Errorf("expected empty tags, got %v", m.Tags)
	}
	if len(m.Actors) != 0 {
		t.Errorf("expected empty actors, got %v", m.Actors)
	}
}

func TestParseNFO_InvalidXML(t *testing.T) {
	_, err := parseNFO(`not xml at all <<<`)
	if err == nil {
		t.Error("expected error for invalid XML, got nil")
	}
}

func TestParseNFO_EmptyString(t *testing.T) {
	_, err := parseNFO("")
	if err == nil {
		t.Error("expected error for empty string, got nil")
	}
}

func TestParseNFO_WhitespaceTrimmed(t *testing.T) {
	nfo := `<movie>
		<title>  Spaced Out  </title>
		<plot>  Some plot.  </plot>
		<tagline>  A tagline.  </tagline>
	</movie>`
	m, _ := parseNFO(nfo)
	if m.Title != "Spaced Out" {
		t.Errorf("title whitespace not trimmed: %q", m.Title)
	}
	if m.Plot != "Some plot." {
		t.Errorf("plot whitespace not trimmed: %q", m.Plot)
	}
}

func TestParseNFO_IDTypeCaseInsensitive(t *testing.T) {
	nfo := `<movie>
		<title>T</title>
		<uniqueid type="TMDB">999</uniqueid>
		<uniqueid type="IMDB">tt9999999</uniqueid>
	</movie>`
	m, _ := parseNFO(nfo)
	if m.TmdbID != "999" {
		t.Errorf("tmdb_id case: %q", m.TmdbID)
	}
	if m.ImdbID != "tt9999999" {
		t.Errorf("imdb_id case: %q", m.ImdbID)
	}
}
