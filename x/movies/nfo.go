package movies

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// nfoMovie is the XML structure for a Kodi/Jellyfin .nfo file.
type nfoMovie struct {
	XMLName    xml.Name      `xml:"movie"`
	Title      string        `xml:"title"`
	SortTitle  string        `xml:"sorttitle"`
	Year       int           `xml:"year"`
	Plot       string        `xml:"plot"`
	Tagline    string        `xml:"tagline"`
	Runtime    int           `xml:"runtime"`
	Rating     float64       `xml:"rating"`
	UniqueIDs  []nfoUniqueID `xml:"uniqueid"`
	Genres     []string      `xml:"genre"`
	Tags       []string      `xml:"tag"`
	Actors     []nfoActor    `xml:"actor"`
	Thumbs     []nfoThumb    `xml:"thumb"`
	Fanart     nfoFanart     `xml:"fanart"`
	LastPlayed string        `xml:"lastplayed"`
	Set        nfoSet        `xml:"set"`
}

type nfoSet struct {
	Name string `xml:"name"`
}

type nfoUniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr"`
	Value   string `xml:",chardata"`
}

type nfoActor struct {
	Name  string `xml:"name"`
	Role  string `xml:"role"`
	Order int    `xml:"order"`
}

type nfoThumb struct {
	Aspect string `xml:"aspect,attr"`
	URL    string `xml:",chardata"`
}

type nfoFanart struct {
	Thumbs []nfoFanartThumb `xml:"thumb"`
}

type nfoFanartThumb struct {
	URL string `xml:",chardata"`
}

// parseNFO parses a Kodi/Jellyfin .nfo XML string into a movie struct.
// Genres and tags are both mapped to movie.Tags (deduped).
func parseNFO(content string) (movie, error) {
	var nfo nfoMovie
	if err := xml.Unmarshal([]byte(content), &nfo); err != nil {
		return movie{}, fmt.Errorf("parse nfo: %w", err)
	}

	var m movie
	m.Title = strings.TrimSpace(nfo.Title)
	m.SortTitle = strings.TrimSpace(nfo.SortTitle)
	m.Year = nfo.Year
	m.Plot = strings.TrimSpace(nfo.Plot)
	m.Tagline = strings.TrimSpace(nfo.Tagline)
	m.Runtime = nfo.Runtime
	m.Rating = nfo.Rating
	m.LastPlayed = strings.TrimSpace(nfo.LastPlayed)
	m.SetName = strings.TrimSpace(nfo.Set.Name)

	// Extract TMDB and IMDB IDs from uniqueid elements
	for _, uid := range nfo.UniqueIDs {
		switch strings.ToLower(uid.Type) {
		case "tmdb":
			m.TmdbID = strings.TrimSpace(uid.Value)
		case "imdb":
			m.ImdbID = strings.TrimSpace(uid.Value)
		}
	}

	// Combine genres and tags, deduped, always lowercase
	seen := map[string]bool{}
	var allTags []string
	for _, g := range nfo.Genres {
		g = strings.ToLower(strings.TrimSpace(g))
		if g != "" && !seen[g] {
			seen[g] = true
			allTags = append(allTags, g)
		}
	}
	for _, t := range nfo.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			allTags = append(allTags, t)
		}
	}
	if allTags == nil {
		allTags = []string{}
	}
	m.Tags = allTags

	// Extract poster URL from thumb elements with aspect="poster"
	for _, thumb := range nfo.Thumbs {
		if strings.ToLower(thumb.Aspect) == "poster" {
			m.PosterURL = strings.TrimSpace(thumb.URL)
			break
		}
	}

	// Extract fanart URL
	if len(nfo.Fanart.Thumbs) > 0 {
		m.FanartURL = strings.TrimSpace(nfo.Fanart.Thumbs[0].URL)
	}

	// Build actors
	for i, a := range nfo.Actors {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		order := a.Order
		if order == 0 {
			order = i
		}
		m.Actors = append(m.Actors, movieActor{
			Name:      name,
			Role:      strings.TrimSpace(a.Role),
			SortOrder: order,
		})
	}
	if m.Actors == nil {
		m.Actors = []movieActor{}
	}

	return m, nil
}
