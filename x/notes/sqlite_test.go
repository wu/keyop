package notes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openTestDB creates a temporary notes database with the schema initialised.
func openTestNotesDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "notes.db")
	db, err := openNotesDB(dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS notes (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id    INTEGER NOT NULL DEFAULT 0,
		title      TEXT    NOT NULL DEFAULT '',
		content    TEXT    NOT NULL DEFAULT '',
		tags       TEXT    NOT NULL DEFAULT '',
		color      TEXT    NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
		updated_at DATETIME DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
	)`)
	require.NoError(t, err)
	_ = db.Close()
	return dbPath
}

// insertTestNote inserts a note and returns its id.
func insertTestNote(t *testing.T, dbPath, title, content, tags string) int64 {
	t.Helper()
	result, err := createNotesEntry(dbPath, title, content, tags)
	require.NoError(t, err)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	id, ok := m["id"].(int64)
	require.True(t, ok, "expected int64 id, got %T", m["id"])
	return id
}

// TestGetNotesListPagination verifies that offset/limit work correctly and
// that 'total' always reflects the full matching count — not just the current
// page size.  This is the backend contract that the frontend pagination relies
// on; a regression here would cause the UI to miscalculate total pages.
func TestGetNotesListPagination(t *testing.T) {
	dbPath := openTestNotesDB(t)

	// Insert 7 notes so we can page through them with pageSize=3.
	for i := 1; i <= 7; i++ {
		insertTestNote(t, dbPath, fmt.Sprintf("Note %d", i), "content", "")
	}

	// Page 1 — offset 0, limit 3.
	res, err := getNotesList(dbPath, "", false, "", 3, 0)
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.Equal(t, 7, m["total"], "total must be full count on page 1")
	assert.Len(t, m["notes"], 3, "page 1 should have 3 notes")

	// Page 2 — offset 3, limit 3.
	res, err = getNotesList(dbPath, "", false, "", 3, 3)
	require.NoError(t, err)
	m = res.(map[string]any)
	assert.Equal(t, 7, m["total"], "total must be full count on page 2")
	assert.Len(t, m["notes"], 3, "page 2 should have 3 notes")

	// Page 3 (last, partial) — offset 6, limit 3.
	res, err = getNotesList(dbPath, "", false, "", 3, 6)
	require.NoError(t, err)
	m = res.(map[string]any)
	assert.Equal(t, 7, m["total"], "total must be full count on last page")
	assert.Len(t, m["notes"], 1, "last page should have 1 note")
}

// TestGetNotesListPaginationWithSearch verifies that pagination works correctly
// when combined with a search filter, and that 'total' reflects the filtered
// count (not the unfiltered count) so the frontend computes the right number
// of pages.
func TestGetNotesListPaginationWithSearch(t *testing.T) {
	dbPath := openTestNotesDB(t)

	for i := 1; i <= 5; i++ {
		insertTestNote(t, dbPath, fmt.Sprintf("Alpha %d", i), "content", "")
	}
	for i := 1; i <= 3; i++ {
		insertTestNote(t, dbPath, fmt.Sprintf("Beta %d", i), "content", "")
	}

	// Search for "Alpha" — 5 matches, page 1 of 2 with pageSize=3.
	res, err := getNotesList(dbPath, "Alpha", false, "", 3, 0)
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.Equal(t, 5, m["total"], "total should reflect search-filtered count")
	assert.Len(t, m["notes"], 3)

	// Page 2 — offset 3, still searching "Alpha".
	res, err = getNotesList(dbPath, "Alpha", false, "", 3, 3)
	require.NoError(t, err)
	m = res.(map[string]any)
	assert.Equal(t, 5, m["total"], "total must stay consistent across pages")
	assert.Len(t, m["notes"], 2, "last page of filtered results should have 2 notes")
}

// TestGetNotesListPaginationDoesNotResetOnLayoutChange is a documentation test
// that records the regression: the frontend's recalcPageSize() was resetting
// currentPage to 1 whenever the list container was resized (e.g. when a note
// detail panel opened).  The backend contract is that callers may freely change
// limit/offset without affecting the total count, so changing pageSize mid-
// session must clamp (not reset) the current page.
//
// This test verifies the backend half of that contract: requesting the same
// search with a different pageSize still returns the correct total and items.
func TestGetNotesListPaginationPageSizeChange(t *testing.T) {
	dbPath := openTestNotesDB(t)

	for i := 1; i <= 10; i++ {
		insertTestNote(t, dbPath, fmt.Sprintf("Note %d", i), "content", "")
	}

	// User is on "page 2" with pageSize=4 (offset=4) — sees notes 5-8.
	res, err := getNotesList(dbPath, "", false, "", 4, 4)
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.Equal(t, 10, m["total"])
	assert.Len(t, m["notes"], 4)

	// Layout changes: pageSize becomes 5.  The frontend clamps currentPage so
	// the new offset is 5 (page 2 of 5 with pageSize=5).  The backend must
	// return correct results for offset=5, limit=5.
	res, err = getNotesList(dbPath, "", false, "", 5, 5)
	require.NoError(t, err)
	m = res.(map[string]any)
	assert.Equal(t, 10, m["total"], "total unchanged after pageSize change")
	assert.Len(t, m["notes"], 5, "second page with new pageSize=5 should have 5 notes")

	notes := m["notes"].([]map[string]any)

	// Crucially, both calls used non-zero offsets, confirming the caller is
	// expected to clamp rather than reset to offset=0. We verify the second
	// page (offset=5) is distinct from the first page (offset=0).
	firstPage, err := getNotesList(dbPath, "", false, "", 5, 0)
	require.NoError(t, err)
	firstIDs := map[any]bool{}
	for _, n := range firstPage.(map[string]any)["notes"].([]map[string]any) {
		firstIDs[n["id"]] = true
	}
	for _, n := range notes {
		assert.False(t, firstIDs[n["id"]], "second page note should not appear in first page")
	}
}

// TestGetNotesListEmptyPage verifies that requesting a page beyond the last
// returns an empty notes slice (not an error) with the correct total — so the
// frontend can detect it has gone past the end and clamp gracefully.
func TestGetNotesListEmptyPage(t *testing.T) {
	dbPath := openTestNotesDB(t)

	for i := 1; i <= 3; i++ {
		insertTestNote(t, dbPath, fmt.Sprintf("Note %d", i), "content", "")
	}

	// Offset 10 is well past the end.
	res, err := getNotesList(dbPath, "", false, "", 3, 10)
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.Equal(t, 3, m["total"], "total must still reflect full count even for out-of-range page")
	notes := m["notes"]
	assert.Nil(t, notes, "notes should be nil/empty for out-of-range offset")
}

// TestGetNotesListOpenNotePreservedAcrossPages verifies that the 'total' count
// returned is independent of which note is currently open — the backend has no
// concept of a "selected note", so total is always the filtered count.
// This directly validates the backend half of the pagination-on-note-open bug.
func TestGetNotesListTotalIndependentOfOpenNote(t *testing.T) {
	dbPath := openTestNotesDB(t)

	// Insert 15 notes matching "search" so we have 3 pages of 5.
	for i := 1; i <= 15; i++ {
		insertTestNote(t, dbPath, fmt.Sprintf("Searchable %d", i), "content", "")
	}

	// Simulate: user is on page 3, has a note open.
	// The open note is on page 1 (offset 0), but we're requesting page 3 (offset 10).
	// total must still be 15 — not reset because a note from a different page is open.
	res, err := getNotesList(dbPath, "Searchable", false, "", 5, 10)
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.Equal(t, 15, m["total"], "total must be 15 regardless of which note is open")
	assert.Len(t, m["notes"], 5, "page 3 should return 5 notes")
}

// TestDeleteAndReloadPreservesOS ensures that after deleting a temporary file
// the OS doesn't interfere with note lookups. (Sanity test for test helpers.)
func TestOpenTestNotesDB(t *testing.T) {
	dbPath := openTestNotesDB(t)
	_, err := os.Stat(dbPath)
	assert.NoError(t, err)
}

// --- getTagCounts ---

func TestGetTagCounts_Empty(t *testing.T) {
	dbPath := openTestNotesDB(t)
	res, err := getTagCounts(dbPath, "", false)
	require.NoError(t, err)
	m := res.(map[string]any)
	counts := m["counts"].(map[string]int)
	assert.Equal(t, 0, counts["all"])
	assert.Equal(t, 0, counts["untagged"])
}

func TestGetTagCounts_WithTags(t *testing.T) {
	dbPath := openTestNotesDB(t)
	insertTestNote(t, dbPath, "Note A", "", "go,test")
	insertTestNote(t, dbPath, "Note B", "", "go")
	insertTestNote(t, dbPath, "Note C", "", "")

	res, err := getTagCounts(dbPath, "", false)
	require.NoError(t, err)
	m := res.(map[string]any)
	counts := m["counts"].(map[string]int)

	assert.Equal(t, 3, counts["all"], "all count should equal total notes")
	assert.Equal(t, 1, counts["untagged"], "one note has no tags")
	assert.Equal(t, 2, counts["go"], "two notes tagged 'go'")
	assert.Equal(t, 1, counts["test"], "one note tagged 'test'")
}

func TestGetTagCounts_WithSearch(t *testing.T) {
	dbPath := openTestNotesDB(t)
	insertTestNote(t, dbPath, "Alpha note", "", "go")
	insertTestNote(t, dbPath, "Beta note", "", "go")
	insertTestNote(t, dbPath, "Alpha other", "", "rust")

	res, err := getTagCounts(dbPath, "Alpha", false)
	require.NoError(t, err)
	m := res.(map[string]any)
	counts := m["counts"].(map[string]int)

	assert.Equal(t, 2, counts["all"], "only 2 notes match search 'Alpha'")
	assert.Equal(t, 1, counts["go"])
	assert.Equal(t, 1, counts["rust"])
	assert.Equal(t, 0, counts["untagged"])
}

// --- getNotesEntry ---

func TestGetNotesEntry_Found(t *testing.T) {
	dbPath := openTestNotesDB(t)
	id := insertTestNote(t, dbPath, "My Title", "My Content", "foo,bar")

	res, err := getNotesEntry(dbPath, id)
	require.NoError(t, err)
	m := res.(map[string]any)

	assert.Equal(t, id, m["id"])
	assert.Equal(t, "My Title", m["title"])
	assert.Equal(t, "My Content", m["content"])
	assert.Equal(t, "foo,bar", m["tags"])
	assert.NotEmpty(t, m["created_at"])
	assert.NotEmpty(t, m["updated_at"])
}

func TestGetNotesEntry_NotFound(t *testing.T) {
	dbPath := openTestNotesDB(t)
	_, err := getNotesEntry(dbPath, 9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- createNotesEntry ---

func TestCreateNotesEntry_Basic(t *testing.T) {
	dbPath := openTestNotesDB(t)
	res, err := createNotesEntry(dbPath, "New Note", "Hello world", "tag1")
	require.NoError(t, err)
	m := res.(map[string]any)

	assert.NotNil(t, m["id"])
	assert.Equal(t, "New Note", m["title"])
	assert.Equal(t, "Hello world", m["content"])
	assert.Equal(t, "tag1", m["tags"])
	assert.NotEmpty(t, m["created_at"])
}

func TestCreateNotesEntry_DuplicateTitle(t *testing.T) {
	dbPath := openTestNotesDB(t)
	_, err := createNotesEntry(dbPath, "Duplicate", "content", "")
	require.NoError(t, err)

	_, err = createNotesEntry(dbPath, "Duplicate", "other content", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

// --- updateNotesEntry ---

func TestUpdateNotesEntry_Basic(t *testing.T) {
	dbPath := openTestNotesDB(t)
	id := insertTestNote(t, dbPath, "Original", "old content", "old")

	res, err := updateNotesEntry(dbPath, id, "Updated", "new content", "new")
	require.NoError(t, err)
	m := res.(map[string]any)

	assert.Equal(t, id, m["id"])
	assert.Equal(t, "Updated", m["title"])
	assert.Equal(t, "new content", m["content"])
	assert.Equal(t, "new", m["tags"])
	assert.NotEmpty(t, m["updated_at"])

	// Verify persisted
	got, err := getNotesEntry(dbPath, id)
	require.NoError(t, err)
	gm := got.(map[string]any)
	assert.Equal(t, "Updated", gm["title"])
	assert.Equal(t, "new content", gm["content"])
}

func TestUpdateNotesEntry_NotFound(t *testing.T) {
	dbPath := openTestNotesDB(t)
	_, err := updateNotesEntry(dbPath, 9999, "Title", "content", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- deleteNotesEntry ---

func TestDeleteNotesEntry_Basic(t *testing.T) {
	dbPath := openTestNotesDB(t)
	id := insertTestNote(t, dbPath, "To Delete", "content", "")

	res, err := deleteNotesEntry(dbPath, id)
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.Equal(t, true, m["deleted"])

	_, err = getNotesEntry(dbPath, id)
	require.Error(t, err)
}

func TestDeleteNotesEntry_NotFound(t *testing.T) {
	dbPath := openTestNotesDB(t)
	_, err := deleteNotesEntry(dbPath, 9999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- getNoteTitles ---

func TestGetNoteTitles_Empty(t *testing.T) {
	dbPath := openTestNotesDB(t)
	res, err := getNoteTitles(dbPath)
	require.NoError(t, err)
	m := res.(map[string]any)
	assert.Nil(t, m["titles"])
}

func TestGetNoteTitles_Multiple(t *testing.T) {
	dbPath := openTestNotesDB(t)
	insertTestNote(t, dbPath, "Short", "", "")
	insertTestNote(t, dbPath, "A longer title", "", "")
	insertTestNote(t, dbPath, "Medium title", "", "")

	res, err := getNoteTitles(dbPath)
	require.NoError(t, err)
	m := res.(map[string]any)

	// The inner noteTitle type is function-scoped; use JSON to access fields.
	raw, err := json.Marshal(m["titles"])
	require.NoError(t, err)
	var titles []struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	require.NoError(t, json.Unmarshal(raw, &titles))
	require.Len(t, titles, 3)

	// ORDER BY length(title) DESC — longest first.
	assert.Equal(t, "A longer title", titles[0].Title)
	assert.Equal(t, "Medium title", titles[1].Title)
	assert.Equal(t, "Short", titles[2].Title)
}

// --- titleExists ---

func TestTitleExists_Exists(t *testing.T) {
	dbPath := openTestNotesDB(t)
	insertTestNote(t, dbPath, "Existing", "", "")

	db, err := openNotesDB(dbPath)
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck

	exists, err := titleExists(db, "Existing", 0)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestTitleExists_NotExists(t *testing.T) {
	dbPath := openTestNotesDB(t)

	db, err := openNotesDB(dbPath)
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck

	exists, err := titleExists(db, "Ghost", 0)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestTitleExists_ExcludeID(t *testing.T) {
	dbPath := openTestNotesDB(t)
	id := insertTestNote(t, dbPath, "SameTitle", "", "")

	db, err := openNotesDB(dbPath)
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck

	// Excluding the note's own ID should return false (same title is OK for update).
	exists, err := titleExists(db, "SameTitle", id)
	require.NoError(t, err)
	assert.False(t, exists)
}
