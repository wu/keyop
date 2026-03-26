package movies

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // SQLite driver for database/sql
)

const moviesSchema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS movies (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid        TEXT    NOT NULL DEFAULT '' UNIQUE,
    title       TEXT    NOT NULL,
    sort_title  TEXT    NOT NULL DEFAULT '',
    year        INTEGER NOT NULL DEFAULT 0,
    plot        TEXT    NOT NULL DEFAULT '',
    tagline     TEXT    NOT NULL DEFAULT '',
    runtime     INTEGER NOT NULL DEFAULT 0,
    rating      REAL    NOT NULL DEFAULT 0,
    tmdb_id     TEXT    NOT NULL DEFAULT '',
    imdb_id     TEXT    NOT NULL DEFAULT '',
    poster_url  TEXT    NOT NULL DEFAULT '',
    fanart_url  TEXT    NOT NULL DEFAULT '',
    set_name    TEXT    NOT NULL DEFAULT '',
    last_played TEXT    NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS movie_actors (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id    INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    role        TEXT    NOT NULL DEFAULT '',
    sort_order  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS movie_tags (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id    INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    tag         TEXT    NOT NULL,
    UNIQUE(movie_id, tag)
);
`

// migrateMoviesSchema adds columns introduced after the initial schema.
func migrateMoviesSchema(db *sql.DB) error {
	for _, col := range []string{
		`ALTER TABLE movies ADD COLUMN set_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE movies ADD COLUMN last_played TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(col); err != nil && !isDuplicateColumnError(err) {
			return err
		}
	}
	return nil
}

func isDuplicateColumnError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "duplicate column") ||
		strings.Contains(err.Error(), "already exists"))
}

type movie struct {
	ID         int64
	UUID       string
	Title      string
	SortTitle  string
	Year       int
	Plot       string
	Tagline    string
	Runtime    int
	Rating     float64
	TmdbID     string
	ImdbID     string
	PosterURL  string
	FanartURL  string
	SetName    string
	LastPlayed string // RFC3339 or empty
	Tags       []string
	Actors     []movieActor
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type movieActor struct {
	ID        int64
	MovieID   int64
	Name      string
	Role      string
	SortOrder int
}

func insertMovie(db *sql.DB, m movie) (int64, error) {
	if m.UUID == "" {
		m.UUID = uuid.New().String()
	}
	now := time.Now().UTC()
	m.CreatedAt = now
	m.UpdatedAt = now
	if m.Tags == nil {
		m.Tags = []string{}
	}

	res, err := db.Exec(`
		INSERT INTO movies (uuid, title, sort_title, year, plot, tagline, runtime, rating, tmdb_id, imdb_id, poster_url, fanart_url, set_name, last_played, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.UUID, m.Title, m.SortTitle, m.Year, m.Plot, m.Tagline, m.Runtime, m.Rating,
		m.TmdbID, m.ImdbID, m.PosterURL, m.FanartURL, m.SetName, m.LastPlayed,
		m.CreatedAt.Format(time.RFC3339), m.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("insert movie: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	if err := setMovieTags(db, id, m.Tags); err != nil {
		return 0, err
	}
	if err := setMovieActors(db, id, m.Actors); err != nil {
		return 0, err
	}
	return id, nil
}

func updateMovie(db *sql.DB, m movie) error {
	m.UpdatedAt = time.Now().UTC()
	if m.Tags == nil {
		m.Tags = []string{}
	}

	_, err := db.Exec(`
		UPDATE movies SET title=?, sort_title=?, year=?, plot=?, tagline=?, runtime=?, rating=?,
		tmdb_id=?, imdb_id=?, poster_url=?, fanart_url=?, set_name=?, last_played=?, updated_at=?
		WHERE id=?`,
		m.Title, m.SortTitle, m.Year, m.Plot, m.Tagline, m.Runtime, m.Rating,
		m.TmdbID, m.ImdbID, m.PosterURL, m.FanartURL, m.SetName, m.LastPlayed,
		m.UpdatedAt.Format(time.RFC3339), m.ID,
	)
	if err != nil {
		return fmt.Errorf("update movie: %w", err)
	}

	if err := setMovieTags(db, m.ID, m.Tags); err != nil {
		return err
	}
	return setMovieActors(db, m.ID, m.Actors)
}

func deleteMovie(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM movies WHERE id=?`, id)
	return err
}

func updateMoviePoster(db *sql.DB, id int64, posterURL string) error {
	_, err := db.Exec(`UPDATE movies SET poster_url=?, updated_at=? WHERE id=?`,
		posterURL, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func getMovie(db *sql.DB, id int64) (movie, error) {
	var m movie
	var createdAt, updatedAt string
	err := db.QueryRow(`
		SELECT id, uuid, title, sort_title, year, plot, tagline, runtime, rating,
		tmdb_id, imdb_id, poster_url, fanart_url, set_name, last_played, created_at, updated_at
		FROM movies WHERE id=?`, id).Scan(
		&m.ID, &m.UUID, &m.Title, &m.SortTitle, &m.Year, &m.Plot, &m.Tagline,
		&m.Runtime, &m.Rating, &m.TmdbID, &m.ImdbID, &m.PosterURL, &m.FanartURL,
		&m.SetName, &m.LastPlayed, &createdAt, &updatedAt,
	)
	if err != nil {
		return m, fmt.Errorf("get movie: %w", err)
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	tags, err := getMovieTags(db, m.ID)
	if err != nil {
		return m, err
	}
	m.Tags = tags

	actors, err := getMovieActors(db, m.ID)
	if err != nil {
		return m, err
	}
	m.Actors = actors
	return m, nil
}

func getMovieByUUID(db *sql.DB, uid string) (movie, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM movies WHERE uuid=?`, uid).Scan(&id)
	if err != nil {
		return movie{}, fmt.Errorf("get movie by uuid: %w", err)
	}
	return getMovie(db, id)
}

// sortClause returns a safe ORDER BY expression for the given sort key.
func sortClause(sort string) string {
	// Strip leading "the " for alphabetical comparisons.
	titleExpr := `LOWER(COALESCE(NULLIF(sort_title,''), title))`
	titleOrder := `CASE WHEN ` + titleExpr + ` LIKE 'the %' THEN SUBSTR(` + titleExpr + `, 5) ELSE ` + titleExpr + ` END`
	switch sort {
	case "year":
		return `year ASC, ` + titleOrder
	case "runtime":
		return `runtime ASC, ` + titleOrder
	case "last":
		return `CASE WHEN last_played='' OR last_played IS NULL THEN 1 ELSE 0 END, last_played DESC, ` + titleOrder
	default: // "title"
		return titleOrder
	}
}

func listMovies(db *sql.DB, tag, search, setName, sort string, fulltext bool) ([]movie, error) {
	where := "WHERE 1=1"
	args := []any{}

	if search != "" {
		if fulltext {
			where += " AND (title LIKE ? OR plot LIKE ? OR tagline LIKE ?)"
			pat := "%" + search + "%"
			args = append(args, pat, pat, pat)
		} else {
			where += " AND title LIKE ?"
			args = append(args, "%"+search+"%")
		}
	}

	if tag != "" && tag != "all" {
		where += ` AND id IN (SELECT movie_id FROM movie_tags WHERE tag=?)`
		args = append(args, tag)
	}

	if setName != "" {
		where += ` AND set_name=?`
		args = append(args, setName)
	}

	orderBy := sortClause(sort)

	q := `SELECT id, uuid, title, sort_title, year, plot, tagline, runtime, rating,` + //nolint:gosec
		`tmdb_id, imdb_id, poster_url, fanart_url, set_name, last_played, created_at, updated_at
		FROM movies ` + where + ` ORDER BY ` + orderBy
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list movies: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var movies []movie
	for rows.Next() {
		var m movie
		var createdAt, updatedAt string
		if err := rows.Scan(
			&m.ID, &m.UUID, &m.Title, &m.SortTitle, &m.Year, &m.Plot, &m.Tagline,
			&m.Runtime, &m.Rating, &m.TmdbID, &m.ImdbID, &m.PosterURL, &m.FanartURL,
			&m.SetName, &m.LastPlayed, &createdAt, &updatedAt,
		); err != nil {
			continue
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		movies = append(movies, m)
	}

	// Load tags for each movie
	for i := range movies {
		tags, err := getMovieTags(db, movies[i].ID)
		if err != nil {
			return nil, err
		}
		movies[i].Tags = tags
		if movies[i].Tags == nil {
			movies[i].Tags = []string{}
		}
	}

	return movies, nil
}

// listMoviesByActor returns all movies featuring the named actor.
func listMoviesByActor(db *sql.DB, actorName, sort string) ([]movie, error) {
	orderBy := sortClause(sort)
	q := `SELECT id, uuid, title, sort_title, year, plot, tagline, runtime, rating,` + //nolint:gosec
		`tmdb_id, imdb_id, poster_url, fanart_url, set_name, last_played, created_at, updated_at
		FROM movies
		WHERE id IN (SELECT movie_id FROM movie_actors WHERE LOWER(name)=LOWER(?))
		ORDER BY ` + orderBy
	rows, err := db.Query(q, actorName)
	if err != nil {
		return nil, fmt.Errorf("list movies by actor: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var movies []movie
	for rows.Next() {
		var m movie
		var createdAt, updatedAt string
		if err := rows.Scan(
			&m.ID, &m.UUID, &m.Title, &m.SortTitle, &m.Year, &m.Plot, &m.Tagline,
			&m.Runtime, &m.Rating, &m.TmdbID, &m.ImdbID, &m.PosterURL, &m.FanartURL,
			&m.SetName, &m.LastPlayed, &createdAt, &updatedAt,
		); err != nil {
			continue
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		tags, _ := getMovieTags(db, m.ID)
		if tags == nil {
			tags = []string{}
		}
		m.Tags = tags
		movies = append(movies, m)
	}
	return movies, nil
}

func getTagCounts(db *sql.DB, search string, fulltext bool) (map[string]any, error) {
	where := ""
	args := []any{}
	if search != "" {
		if fulltext {
			where = " WHERE movie_id IN (SELECT id FROM movies WHERE title LIKE ? OR plot LIKE ? OR tagline LIKE ?)"
			pat := "%" + search + "%"
			args = append(args, pat, pat, pat)
		} else {
			where = " WHERE movie_id IN (SELECT id FROM movies WHERE title LIKE ?)"
			args = append(args, "%"+search+"%")
		}
	}

	// Total movie count (respecting search filter).
	totalWhere := ""
	totalArgs := []any{}
	if search != "" {
		if fulltext {
			totalWhere = " WHERE title LIKE ? OR plot LIKE ? OR tagline LIKE ?"
			pat := "%" + search + "%"
			totalArgs = append(totalArgs, pat, pat, pat)
		} else {
			totalWhere = " WHERE title LIKE ?"
			totalArgs = append(totalArgs, "%"+search+"%")
		}
	}
	var total int
	_ = db.QueryRow(`SELECT COUNT(*) FROM movies`+totalWhere, totalArgs...).Scan(&total)

	rows, err := db.Query(`
		SELECT tag, COUNT(*) as cnt FROM movie_tags`+where+`
		GROUP BY tag ORDER BY cnt DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("get tag counts: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var counts []map[string]any
	for rows.Next() {
		var tag string
		var cnt int
		if err := rows.Scan(&tag, &cnt); err != nil {
			continue
		}
		counts = append(counts, map[string]any{"tag": tag, "count": cnt})
	}
	if counts == nil {
		counts = []map[string]any{}
	}
	return map[string]any{"counts": counts, "total": total}, nil
}

func setMovieTags(db *sql.DB, movieID int64, tags []string) error {
	if _, err := db.Exec(`DELETE FROM movie_tags WHERE movie_id=?`, movieID); err != nil {
		return fmt.Errorf("delete movie tags: %w", err)
	}
	for _, tag := range tags {
		tag = trimSpace(tag)
		if tag == "" {
			continue
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO movie_tags (movie_id, tag) VALUES (?, ?)`, movieID, tag); err != nil {
			return fmt.Errorf("insert movie tag: %w", err)
		}
	}
	return nil
}

func setMovieActors(db *sql.DB, movieID int64, actors []movieActor) error {
	if _, err := db.Exec(`DELETE FROM movie_actors WHERE movie_id=?`, movieID); err != nil {
		return fmt.Errorf("delete movie actors: %w", err)
	}
	for _, a := range actors {
		if _, err := db.Exec(
			`INSERT INTO movie_actors (movie_id, name, role, sort_order) VALUES (?, ?, ?, ?)`,
			movieID, a.Name, a.Role, a.SortOrder,
		); err != nil {
			return fmt.Errorf("insert movie actor: %w", err)
		}
	}
	return nil
}

func getMovieTags(db *sql.DB, movieID int64) ([]string, error) {
	rows, err := db.Query(`SELECT tag FROM movie_tags WHERE movie_id=? ORDER BY tag`, movieID)
	if err != nil {
		return nil, fmt.Errorf("get movie tags: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err == nil {
			tags = append(tags, tag)
		}
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

func getMovieActors(db *sql.DB, movieID int64) ([]movieActor, error) {
	rows, err := db.Query(`
		SELECT id, movie_id, name, role, sort_order FROM movie_actors
		WHERE movie_id=? ORDER BY sort_order, name`, movieID)
	if err != nil {
		return nil, fmt.Errorf("get movie actors: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var actors []movieActor
	for rows.Next() {
		var a movieActor
		if err := rows.Scan(&a.ID, &a.MovieID, &a.Name, &a.Role, &a.SortOrder); err == nil {
			actors = append(actors, a)
		}
	}
	if actors == nil {
		actors = []movieActor{}
	}
	return actors, nil
}

func trimSpace(s string) string {
	result := ""
	inSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			inSpace = true
		} else {
			if inSpace && result != "" {
				result += " "
			}
			result += string(r)
			inSpace = false
		}
	}
	return result
}
