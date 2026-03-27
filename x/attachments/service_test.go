package attachments

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"keyop/core"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// ---- helpers ----------------------------------------------------------------

func openTestAttachmentsDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.sql")+"?_journal=WAL&_timeout=5000")
	require.NoError(t, err)
	_, err = db.Exec(attachmentsSchema)
	require.NoError(t, err)
	require.NoError(t, migrateAttachmentsSchema(db))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	uploadDir := filepath.Join(dir, "uploads")
	dbPath := filepath.Join(dir, "attachments.sql")

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})

	svc := &Service{
		Deps:      deps,
		uploadDir: uploadDir,
		dbPath:    dbPath,
	}
	require.NoError(t, svc.Initialize())
	return svc
}

func insertTestAttachment(t *testing.T, db *sql.DB, orig, stored, dateDir string) attachment {
	t.Helper()
	a := attachment{
		UUID:             "test-uuid-" + orig,
		OriginalFilename: orig,
		StoredFilename:   stored,
		DateDir:          dateDir,
		MimeType:         "text/plain",
		Size:             42,
		UploadedAt:       time.Now().UTC(),
	}
	id, err := insertAttachment(db, a)
	require.NoError(t, err)
	require.Greater(t, id, int64(0))
	a.ID = id
	return a
}

// ---- Part 1: pure function tests --------------------------------------------

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"hello world.pdf", "hello_world.pdf"},
		{"my-file_2024.txt", "my_file_2024.txt"}, // hyphen replaced
		{"file (1).jpg", "file__1_.jpg"},
		{"test", "test"},                 // no extension
		{"", "attachment"},               // empty → fallback
		{".pdf", "attachment.pdf"},       // empty base → fallback
		{"notes.tar.gz", "notes.tar.gz"}, // dot in base preserved
		{"ABC_123.PNG", "ABC_123.PNG"},
		{"résumé.docx", "r_sum_.docx"}, // non-ASCII replaced
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeFilename(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "file.txt")

	// File doesn't exist yet — path returned unchanged.
	assert.Equal(t, base, uniquePath(base))

	// Create the file; uniquePath should return _1 variant.
	require.NoError(t, os.WriteFile(base, []byte("x"), 0600))
	want1 := filepath.Join(dir, "file_1.txt")
	assert.Equal(t, want1, uniquePath(base))

	// Create _1 as well; should return _2.
	require.NoError(t, os.WriteFile(want1, []byte("x"), 0600))
	want2 := filepath.Join(dir, "file_2.txt")
	assert.Equal(t, want2, uniquePath(base))
}

func TestUniquePath_NoExtension(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "readme")
	require.NoError(t, os.WriteFile(base, []byte("x"), 0600))
	assert.Equal(t, filepath.Join(dir, "readme_1"), uniquePath(base))
}

// ---- Part 2: SQLite integration tests ---------------------------------------

func TestInsertAttachment(t *testing.T) {
	db := openTestAttachmentsDB(t)
	a := attachment{
		UUID:             "abc-123",
		OriginalFilename: "hello.txt",
		StoredFilename:   "hello.txt",
		DateDir:          "2024-01-01",
		MimeType:         "text/plain",
		Size:             100,
		UploadedAt:       time.Now().UTC(),
	}
	id, err := insertAttachment(db, a)
	require.NoError(t, err)
	assert.Greater(t, id, int64(0))
}

func TestListAttachments_Empty(t *testing.T) {
	db := openTestAttachmentsDB(t)
	results, err := listAttachments(db)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestListAttachments_Multiple(t *testing.T) {
	db := openTestAttachmentsDB(t)

	a1 := attachment{
		UUID: "uuid-1", OriginalFilename: "a.txt", StoredFilename: "a.txt",
		DateDir: "2024-01-01", MimeType: "text/plain", Size: 1,
		UploadedAt: time.Now().Add(-time.Hour).UTC(),
	}
	a2 := attachment{
		UUID: "uuid-2", OriginalFilename: "b.txt", StoredFilename: "b.txt",
		DateDir: "2024-01-01", MimeType: "text/plain", Size: 2,
		UploadedAt: time.Now().UTC(),
	}
	_, err := insertAttachment(db, a1)
	require.NoError(t, err)
	_, err = insertAttachment(db, a2)
	require.NoError(t, err)

	results, err := listAttachments(db)
	require.NoError(t, err)
	require.Len(t, results, 2)
	// newest first
	assert.Equal(t, "uuid-2", results[0].UUID)
	assert.Equal(t, "uuid-1", results[1].UUID)
}

func TestGetAttachmentByUUID(t *testing.T) {
	db := openTestAttachmentsDB(t)
	a := attachment{
		UUID: "find-me", OriginalFilename: "orig.pdf", StoredFilename: "orig.pdf",
		DateDir: "2024-06-01", MimeType: "application/pdf", Size: 512,
		UploadedAt: time.Now().UTC(),
	}
	_, err := insertAttachment(db, a)
	require.NoError(t, err)

	got, err := getAttachmentByUUID(db, "find-me")
	require.NoError(t, err)
	assert.Equal(t, "find-me", got.UUID)
	assert.Equal(t, "orig.pdf", got.OriginalFilename)
	assert.Equal(t, int64(512), got.Size)
}

func TestGetAttachmentByUUID_NotFound(t *testing.T) {
	db := openTestAttachmentsDB(t)
	_, err := getAttachmentByUUID(db, "does-not-exist")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestDeleteAttachmentByUUID(t *testing.T) {
	db := openTestAttachmentsDB(t)
	a := attachment{
		UUID: "del-me", OriginalFilename: "del.txt", StoredFilename: "del.txt",
		DateDir: "2024-01-01", MimeType: "text/plain", Size: 5,
		UploadedAt: time.Now().UTC(),
	}
	_, err := insertAttachment(db, a)
	require.NoError(t, err)

	require.NoError(t, deleteAttachmentByUUID(db, "del-me"))

	_, err = getAttachmentByUUID(db, "del-me")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

// ---- Part 3: service lifecycle tests ----------------------------------------

func TestInitialize_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	uploadDir := filepath.Join(dir, "uploads", "nested")
	dbPath := filepath.Join(dir, "db", "attachments.sql")

	deps := core.Dependencies{}
	deps.SetLogger(&core.FakeLogger{})

	svc := &Service{
		Deps:      deps,
		uploadDir: uploadDir,
		dbPath:    dbPath,
	}
	require.NoError(t, svc.Initialize())

	_, err := os.Stat(uploadDir)
	assert.NoError(t, err, "upload dir should be created")

	_, err = os.Stat(dbPath)
	assert.NoError(t, err, "database file should be created")
}

func TestCheck_AfterInitialize(t *testing.T) {
	svc := newTestService(t)
	assert.NoError(t, svc.Check())
}

func TestCheck_BeforeInitialize(t *testing.T) {
	svc := &Service{}
	err := svc.Check()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

// ---- Part 4: WebUI action tests ---------------------------------------------

func TestHandleWebUIAction_ListFiles_Empty(t *testing.T) {
	svc := newTestService(t)
	result, err := svc.HandleWebUIAction("list-files", nil)
	require.NoError(t, err)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	files, ok := m["files"].([]map[string]any)
	if ok {
		assert.Empty(t, files)
	} else {
		// listFiles returns []map[string]any with capacity 0, which is non-nil
		assert.NotNil(t, m["files"])
		assert.Empty(t, m["files"])
	}
}

func TestHandleWebUIAction_ListFiles_WithRecords(t *testing.T) {
	svc := newTestService(t)
	insertTestAttachment(t, svc.db, "doc.pdf", "doc.pdf", todayDir())

	result, err := svc.HandleWebUIAction("list-files", nil)
	require.NoError(t, err)
	m := result.(map[string]any)
	files := m["files"].([]map[string]any)
	require.Len(t, files, 1)
	assert.Equal(t, "doc.pdf", files[0]["originalFilename"])
}

func TestHandleWebUIAction_DeleteFile(t *testing.T) {
	svc := newTestService(t)
	a := insertTestAttachment(t, svc.db, "todelete.txt", "todelete.txt", todayDir())

	result, err := svc.HandleWebUIAction("delete-file", map[string]any{"id": a.UUID})
	require.NoError(t, err)
	m := result.(map[string]any)
	assert.Equal(t, true, m["deleted"])

	// Record should be gone.
	_, dbErr := getAttachmentByUUID(svc.db, a.UUID)
	assert.ErrorIs(t, dbErr, sql.ErrNoRows)
}

func TestHandleWebUIAction_DeleteFile_MissingID(t *testing.T) {
	svc := newTestService(t)
	result, err := svc.HandleWebUIAction("delete-file", map[string]any{})
	require.NoError(t, err)
	m := result.(map[string]any)
	assert.Contains(t, m, "error")
}

func TestHandleWebUIAction_DeleteFile_NilParams(t *testing.T) {
	svc := newTestService(t)
	result, err := svc.HandleWebUIAction("delete-file", nil)
	require.NoError(t, err)
	m := result.(map[string]any)
	assert.Contains(t, m, "error")
}

func TestHandleWebUIAction_UnknownAction(t *testing.T) {
	svc := newTestService(t)
	result, err := svc.HandleWebUIAction("unknown-action", nil)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// ---- Part 5: HTTP handler tests ---------------------------------------------

// buildUploadRequest creates a multipart POST request with a single file field.
func buildUploadRequest(t *testing.T, filename, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = io.WriteString(fw, content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/attachments/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestHandleUpload(t *testing.T) {
	svc := newTestService(t)

	req := buildUploadRequest(t, "hello world.txt", "file contents here")
	rec := httptest.NewRecorder()
	svc.handleUpload(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotEmpty(t, resp["id"])
	assert.Equal(t, "hello world.txt", resp["originalFilename"])
	assert.Equal(t, "hello_world.txt", resp["storedFilename"])
	assert.Equal(t, float64(18), resp["size"])

	// File should exist on disk.
	dateDir, _ := resp["dateDir"].(string)
	storedFilename, _ := resp["storedFilename"].(string)
	diskPath := filepath.Join(svc.uploadDir, dateDir, storedFilename)
	_, err := os.Stat(diskPath)
	assert.NoError(t, err, "uploaded file should exist on disk")
}

func TestHandleUpload_CustomFilename(t *testing.T) {
	svc := newTestService(t)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "original.txt")
	require.NoError(t, err)
	_, err = io.WriteString(fw, "data")
	require.NoError(t, err)
	require.NoError(t, w.WriteField("filename", "custom name!.txt"))
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/attachments/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	svc.handleUpload(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "custom_name_.txt", resp["storedFilename"])
}

func TestHandleUpload_MissingFileField(t *testing.T) {
	svc := newTestService(t)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/attachments/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	svc.handleUpload(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleServeFile(t *testing.T) {
	svc := newTestService(t)

	// Upload a file first so it exists in the DB and on disk.
	req := buildUploadRequest(t, "serve_me.txt", "serve content")
	rec := httptest.NewRecorder()
	svc.handleUpload(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var uploadResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&uploadResp))
	id := uploadResp["id"].(string)

	// Now serve the file.
	req2 := httptest.NewRequest(http.MethodGet, "/api/attachments/file/"+id, nil)
	req2.SetPathValue("uuid", id)
	rec2 := httptest.NewRecorder()
	svc.handleServeFile(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	disp := rec2.Header().Get("Content-Disposition")
	assert.True(t, strings.HasPrefix(disp, "attachment;"), "expected attachment disposition, got: %s", disp)
	assert.Equal(t, "serve content", rec2.Body.String())
}

func TestHandleServeFile_NotFound(t *testing.T) {
	svc := newTestService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/attachments/file/no-such-uuid", nil)
	req.SetPathValue("uuid", "no-such-uuid")
	rec := httptest.NewRecorder()
	svc.handleServeFile(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlePreviewFile(t *testing.T) {
	svc := newTestService(t)

	req := buildUploadRequest(t, "preview_me.txt", "preview content")
	rec := httptest.NewRecorder()
	svc.handleUpload(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var uploadResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&uploadResp))
	id := uploadResp["id"].(string)

	req2 := httptest.NewRequest(http.MethodGet, "/api/attachments/preview/"+id, nil)
	req2.SetPathValue("uuid", id)
	rec2 := httptest.NewRecorder()
	svc.handlePreviewFile(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
	disp := rec2.Header().Get("Content-Disposition")
	assert.True(t, strings.HasPrefix(disp, "inline;"), "expected inline disposition, got: %s", disp)
	assert.Equal(t, "preview content", rec2.Body.String())
}

func TestHandlePreviewFile_NotFound(t *testing.T) {
	svc := newTestService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/attachments/preview/ghost", nil)
	req.SetPathValue("uuid", "ghost")
	rec := httptest.NewRecorder()
	svc.handlePreviewFile(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleServeFile_EmptyUUID(t *testing.T) {
	svc := newTestService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/attachments/file/", nil)
	req.SetPathValue("uuid", "")
	rec := httptest.NewRecorder()
	svc.handleServeFile(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlePreviewFile_EmptyUUID(t *testing.T) {
	svc := newTestService(t)
	req := httptest.NewRequest(http.MethodGet, "/api/attachments/preview/", nil)
	req.SetPathValue("uuid", "")
	rec := httptest.NewRecorder()
	svc.handlePreviewFile(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
