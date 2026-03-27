package flashcards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"keyop/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── test helpers ────────────────────────────────────────────────────────────

func newTestFlashcardsService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "flashcards.sql")
	var deps core.Dependencies
	deps.SetOsProvider(core.FakeOsProvider{
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			return os.MkdirAll(path, perm)
		},
	})
	svc := &Service{Deps: deps, dbPath: dbPath}
	require.NoError(t, svc.Initialize())
	t.Cleanup(func() { _ = svc.OnShutdown() })
	return svc
}

// parseDue parses a RFC3339Nano UTC string.
func parseDue(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, s)
	require.NoError(t, err, "parseDue: %q", s)
	return ts
}

// assertWithin asserts that got is within delta of want.
func assertWithin(t *testing.T, want, got time.Time, delta time.Duration) {
	t.Helper()
	diff := got.Sub(want)
	if diff < 0 {
		diff = -diff
	}
	assert.LessOrEqualf(t, diff, delta, "expected %v ≈ %v (±%v)", got.Format(time.RFC3339), want.Format(time.RFC3339), delta)
}

// localMidnight returns midnight N days from now in local time, expressed in UTC.
func localMidnight(days int) time.Time {
	now := time.Now().In(time.Local).AddDate(0, 0, days)
	mid := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	return mid.UTC()
}

// createTestCard creates a card via HandleWebUIAction and returns its ID.
func createTestCard(t *testing.T, svc *Service, question, answer, tags string) int64 {
	t.Helper()
	res, err := svc.HandleWebUIAction("create-card", map[string]any{
		"question": question,
		"answer":   answer,
		"tags":     tags,
	})
	require.NoError(t, err)
	m, ok := res.(map[string]any)
	require.True(t, ok)
	id, ok := m["id"].(int64)
	require.True(t, ok, "expected int64 id, got %T: %v", m["id"], m["id"])
	return id
}

// listDueCards returns the cards slice from a list-due action, decoded as []map[string]any.
func listDueCards(t *testing.T, svc *Service, tag string) []map[string]any {
	t.Helper()
	res, err := svc.HandleWebUIAction("list-due", map[string]any{"tag": tag})
	require.NoError(t, err)
	m, ok := res.(map[string]any)
	require.True(t, ok)
	raw, err := json.Marshal(m["cards"])
	require.NoError(t, err)
	var cards []map[string]any
	require.NoError(t, json.Unmarshal(raw, &cards))
	return cards
}

// ─── scheduleAnki: new card ───────────────────────────────────────────────────

func TestScheduleAnki_NewCard_ShowAgain(t *testing.T) {
	before := time.Now()
	dueDate, newEF, newInterval, newReps, newState, newStep := scheduleAnki(2.5, 0, 0, "new", 0, "show_again")
	due := parseDue(t, dueDate)

	assert.Equal(t, "learning", newState)
	assert.Equal(t, int64(0), newStep)
	assert.Equal(t, 2.5, newEF)
	assert.Equal(t, int64(0), newInterval)
	assert.Equal(t, int64(0), newReps)
	assertWithin(t, before.Add(1*time.Minute), due, 5*time.Second)
}

func TestScheduleAnki_NewCard_Hard(t *testing.T) {
	before := time.Now()
	dueDate, newEF, newInterval, newReps, newState, newStep := scheduleAnki(2.5, 0, 0, "new", 0, "hard")
	due := parseDue(t, dueDate)

	assert.Equal(t, "learning", newState)
	assert.Equal(t, int64(0), newStep) // hard does not advance step
	assert.Equal(t, 2.5, newEF)
	assert.Equal(t, int64(0), newInterval)
	assert.Equal(t, int64(0), newReps)
	// avg(1min, 10min) = 5m30s
	assertWithin(t, before.Add(5*time.Minute+30*time.Second), due, 5*time.Second)
}

func TestScheduleAnki_NewCard_Correct(t *testing.T) {
	before := time.Now()
	dueDate, newEF, newInterval, newReps, newState, newStep := scheduleAnki(2.5, 0, 0, "new", 0, "correct")
	due := parseDue(t, dueDate)

	assert.Equal(t, "learning", newState)
	assert.Equal(t, int64(1), newStep)
	assert.Equal(t, 2.5, newEF)
	assert.Equal(t, int64(0), newInterval)
	assert.Equal(t, int64(0), newReps)
	assertWithin(t, before.Add(10*time.Minute), due, 5*time.Second)
}

func TestScheduleAnki_NewCard_Easy(t *testing.T) {
	dueDate, newEF, newInterval, newReps, newState, newStep := scheduleAnki(2.5, 0, 0, "new", 0, "easy")
	due := parseDue(t, dueDate)

	assert.Equal(t, "review", newState)
	assert.Equal(t, int64(0), newStep)
	assert.Equal(t, int64(4), newInterval)
	assert.Equal(t, int64(1), newReps)
	assert.Equal(t, 2.5, newEF)
	want := localMidnight(4)
	assert.Equal(t, want.Format(time.RFC3339Nano), due.UTC().Format(time.RFC3339Nano))
}

// ─── scheduleAnki: learning step 0 (1 min) ───────────────────────────────────

func TestScheduleAnki_LearningStep0_Correct(t *testing.T) {
	before := time.Now()
	dueDate, _, _, _, newState, newStep := scheduleAnki(2.5, 0, 0, "learning", 0, "correct")
	due := parseDue(t, dueDate)

	assert.Equal(t, "learning", newState)
	assert.Equal(t, int64(1), newStep)
	assertWithin(t, before.Add(10*time.Minute), due, 5*time.Second)
}

func TestScheduleAnki_LearningStep0_ShowAgain(t *testing.T) {
	before := time.Now()
	dueDate, _, _, _, newState, newStep := scheduleAnki(2.5, 0, 0, "learning", 0, "show_again")
	due := parseDue(t, dueDate)

	assert.Equal(t, "learning", newState)
	assert.Equal(t, int64(0), newStep)
	assertWithin(t, before.Add(1*time.Minute), due, 5*time.Second)
}

// ─── scheduleAnki: learning step 1 (10 min, last step) ───────────────────────

func TestScheduleAnki_LearningStep1_Correct(t *testing.T) {
	dueDate, _, newInterval, newReps, newState, newStep := scheduleAnki(2.5, 0, 0, "learning", 1, "correct")
	due := parseDue(t, dueDate)

	assert.Equal(t, "review", newState)
	assert.Equal(t, int64(0), newStep)
	assert.Equal(t, int64(1), newInterval)
	assert.Equal(t, int64(1), newReps)
	want := localMidnight(1)
	assert.Equal(t, want.Format(time.RFC3339Nano), due.UTC().Format(time.RFC3339Nano))
}

func TestScheduleAnki_LearningStep1_Easy(t *testing.T) {
	dueDate, _, newInterval, newReps, newState, newStep := scheduleAnki(2.5, 0, 0, "learning", 1, "easy")
	due := parseDue(t, dueDate)

	assert.Equal(t, "review", newState)
	assert.Equal(t, int64(0), newStep)
	assert.Equal(t, int64(4), newInterval)
	assert.Equal(t, int64(1), newReps)
	want := localMidnight(4)
	assert.Equal(t, want.Format(time.RFC3339Nano), due.UTC().Format(time.RFC3339Nano))
}

// ─── scheduleAnki: review card ────────────────────────────────────────────────

func TestScheduleAnki_Review_Correct(t *testing.T) {
	// ef=2.5, interval=10, reps=3
	// hardInterval = max(11, round(10*1.2)) = max(11,12) = 12
	// newInterval  = max(13, round(10*2.5)) = max(13,25) = 25
	dueDate, newEF, newInterval, newReps, newState, _ := scheduleAnki(2.5, 10, 3, "review", 0, "correct")
	due := parseDue(t, dueDate)

	assert.Equal(t, "review", newState)
	assert.Equal(t, 2.5, newEF) // unchanged on correct
	assert.Equal(t, int64(25), newInterval)
	assert.Equal(t, int64(4), newReps)
	want := localMidnight(25)
	assert.Equal(t, want.Format(time.RFC3339Nano), due.UTC().Format(time.RFC3339Nano))
}

func TestScheduleAnki_Review_Easy(t *testing.T) {
	// ef=2.5, interval=10, reps=3
	// newEF = 2.5 + 0.15 = 2.65
	// hardInterval    = max(11, round(10*1.2))     = 12
	// correctInterval = max(13, round(10*2.5))     = 25
	// newInterval     = max(26, round(10*2.5*1.3)) = max(26, 33) = 33
	dueDate, newEF, newInterval, newReps, newState, _ := scheduleAnki(2.5, 10, 3, "review", 0, "easy")
	due := parseDue(t, dueDate)

	assert.Equal(t, "review", newState)
	assert.InDelta(t, 2.65, newEF, 0.001)
	assert.Equal(t, int64(33), newInterval)
	assert.Equal(t, int64(4), newReps)
	want := localMidnight(33)
	assert.Equal(t, want.Format(time.RFC3339Nano), due.UTC().Format(time.RFC3339Nano))
}

func TestScheduleAnki_Review_Hard(t *testing.T) {
	// ef=2.5, interval=10, reps=3
	// newEF      = 2.5 - 0.15 = 2.35
	// newInterval = max(11, round(10*1.2)) = max(11, 12) = 12
	dueDate, newEF, newInterval, newReps, newState, _ := scheduleAnki(2.5, 10, 3, "review", 0, "hard")
	due := parseDue(t, dueDate)

	assert.Equal(t, "review", newState)
	assert.InDelta(t, 2.35, newEF, 0.001)
	assert.Equal(t, int64(12), newInterval)
	assert.Equal(t, int64(4), newReps)
	want := localMidnight(12)
	assert.Equal(t, want.Format(time.RFC3339Nano), due.UTC().Format(time.RFC3339Nano))
}

func TestScheduleAnki_Review_ShowAgain(t *testing.T) {
	// ef=2.5, interval=10, reps=3 → lapse: ef decreases, interval resets to 1, state=relearning
	before := time.Now()
	dueDate, newEF, newInterval, newReps, newState, newStep := scheduleAnki(2.5, 10, 3, "review", 0, "show_again")
	due := parseDue(t, dueDate)

	assert.Equal(t, "relearning", newState)
	assert.Equal(t, int64(0), newStep)
	assert.InDelta(t, 2.30, newEF, 0.001)
	assert.Equal(t, int64(1), newInterval) // max(1, round(10*0)) = 1
	assert.Equal(t, int64(4), newReps)     // reps still incremented in review block
	assertWithin(t, before.Add(10*time.Minute), due, 5*time.Second)
}

func TestScheduleAnki_Review_EaseFactor_Floor(t *testing.T) {
	// ef already at floor; show_again must not push it below 1.3
	_, newEF, _, _, _, _ := scheduleAnki(1.3, 10, 3, "review", 0, "show_again")
	assert.Equal(t, 1.3, newEF)
}

func TestScheduleAnki_Review_EaseFactor_Floor_Hard(t *testing.T) {
	// ef near floor; hard must not push it below 1.3
	_, newEF, _, _, _, _ := scheduleAnki(1.4, 10, 3, "review", 0, "hard")
	assert.Equal(t, 1.3, newEF) // 1.4 - 0.15 = 1.25 → clamped to 1.3
}

// ─── scheduleAnki: relearning ─────────────────────────────────────────────────

func TestScheduleAnki_Relearning_Correct(t *testing.T) {
	// After lapse the interval was already set to 1; completing relearn returns to review.
	dueDate, _, newInterval, newReps, newState, newStep := scheduleAnki(2.3, 1, 3, "relearning", 0, "correct")
	due := parseDue(t, dueDate)

	assert.Equal(t, "review", newState)
	assert.Equal(t, int64(0), newStep)
	assert.Equal(t, int64(4), newReps)
	assert.Equal(t, int64(1), newInterval)
	want := localMidnight(1)
	assert.Equal(t, want.Format(time.RFC3339Nano), due.UTC().Format(time.RFC3339Nano))
}

func TestScheduleAnki_Relearning_ShowAgain(t *testing.T) {
	before := time.Now()
	dueDate, _, _, _, newState, newStep := scheduleAnki(2.3, 1, 3, "relearning", 0, "show_again")
	due := parseDue(t, dueDate)

	assert.Equal(t, "relearning", newState)
	assert.Equal(t, int64(0), newStep)
	assertWithin(t, before.Add(10*time.Minute), due, 5*time.Second)
}

// ─── tag helpers ─────────────────────────────────────────────────────────────

func TestNormalizeTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"spaces only", "   ", ""},
		{"single tag", "go", "go"},
		{"trims whitespace", "  go  , lang  ", "go,lang"},
		{"no extra commas", "a,b,c", "a,b,c"},
		{"empty segments dropped", "a,,b", "a,b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeTags(tc.input))
		})
	}
}

func TestSplitTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty string", "", nil},
		{"whitespace only", "   ", nil},
		{"single tag", "go", []string{"go"}},
		{"multiple tags", "go,lang,test", []string{"go", "lang", "test"}},
		{"trims whitespace", " go , lang ", []string{"go", "lang"}},
		{"empty segments dropped", "a,,b", []string{"a", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, splitTags(tc.input))
		})
	}
}

// ─── WebUI + SQLite integration tests ────────────────────────────────────────

func TestFlashcards_CreateAndListDue(t *testing.T) {
	svc := newTestFlashcardsService(t)

	id := createTestCard(t, svc, "What is Go?", "A programming language.", "go")
	require.Greater(t, id, int64(0))

	cards := listDueCards(t, svc, "")
	require.Len(t, cards, 1)
	assert.Equal(t, "What is Go?", cards[0]["question"])
	assert.Equal(t, "A programming language.", cards[0]["answer"])
}

func TestFlashcards_ListTags(t *testing.T) {
	svc := newTestFlashcardsService(t)
	createTestCard(t, svc, "Q1", "A1", "math")
	createTestCard(t, svc, "Q2", "A2", "math,algebra")
	createTestCard(t, svc, "Q3", "A3", "algebra")

	res, err := svc.HandleWebUIAction("list-tags", nil)
	require.NoError(t, err)
	m, ok := res.(map[string]any)
	require.True(t, ok)

	assert.Equal(t, 3, m["allTotal"])
	assert.Equal(t, 3, m["allDue"])

	raw, err := json.Marshal(m["tags"])
	require.NoError(t, err)
	var tagInfos []map[string]any
	require.NoError(t, json.Unmarshal(raw, &tagInfos))

	tagMap := map[string]map[string]any{}
	for _, ti := range tagInfos {
		tagMap[ti["tag"].(string)] = ti
	}

	// "math" appears in Q1 and Q2 → total 2
	require.Contains(t, tagMap, "math")
	assert.Equal(t, float64(2), tagMap["math"]["total"])

	// "algebra" appears in Q2 and Q3 → total 2
	require.Contains(t, tagMap, "algebra")
	assert.Equal(t, float64(2), tagMap["algebra"]["total"])
}

func TestFlashcards_Review_UpdatesSchedule(t *testing.T) {
	svc := newTestFlashcardsService(t)
	id := createTestCard(t, svc, "Q", "A", "")

	// Card is due immediately; confirm it's in the due list.
	cards := listDueCards(t, svc, "")
	require.Len(t, cards, 1)

	// Review with "correct": card graduates to review state with 1-day interval.
	res, err := svc.HandleWebUIAction("review", map[string]any{
		"id":     float64(id),
		"rating": "correct",
	})
	require.NoError(t, err)
	rm, ok := res.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", rm["status"])

	nextDue, ok := rm["nextDue"].(string)
	require.True(t, ok, "expected nextDue string")
	due := parseDue(t, nextDue)
	// Due date should be in the future (at least a few minutes from now).
	assert.True(t, due.After(time.Now()), "nextDue should be in the future")

	// After reviewing, card should no longer appear in the due list.
	cards = listDueCards(t, svc, "")
	assert.Empty(t, cards, "card should not be due immediately after review")
}

func TestFlashcards_UpdateCard(t *testing.T) {
	svc := newTestFlashcardsService(t)
	id := createTestCard(t, svc, "Original Q", "Original A", "old")

	res, err := svc.HandleWebUIAction("update-card", map[string]any{
		"id":       float64(id),
		"question": "Updated Q",
		"answer":   "Updated A",
		"tags":     "new",
	})
	require.NoError(t, err)
	m, ok := res.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", m["status"])

	cards := listDueCards(t, svc, "")
	require.Len(t, cards, 1)
	assert.Equal(t, "Updated Q", cards[0]["question"])
	assert.Equal(t, "Updated A", cards[0]["answer"])
}

func TestFlashcards_DeleteCard(t *testing.T) {
	svc := newTestFlashcardsService(t)
	id := createTestCard(t, svc, "To delete", "Gone soon", "")

	cards := listDueCards(t, svc, "")
	require.Len(t, cards, 1)

	res, err := svc.HandleWebUIAction("delete-card", map[string]any{"id": float64(id)})
	require.NoError(t, err)
	m, ok := res.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ok", m["status"])

	cards = listDueCards(t, svc, "")
	assert.Empty(t, cards, "card should be gone after deletion")
}

func TestFlashcards_PreviewSchedule(t *testing.T) {
	svc := newTestFlashcardsService(t)
	id := createTestCard(t, svc, "Q", "A", "")

	res, err := svc.HandleWebUIAction("preview-schedule", map[string]any{"id": float64(id)})
	require.NoError(t, err)

	preview, ok := res.(map[string]string)
	require.True(t, ok, "expected map[string]string, got %T", res)

	// All four ratings should produce a non-empty due date.
	for _, rating := range []string{"show_again", "hard", "correct", "easy"} {
		assert.NotEmpty(t, preview[rating], "rating %q should have a due date", rating)
		_, err := time.Parse(time.RFC3339Nano, preview[rating])
		assert.NoError(t, err, "rating %q due date should be valid RFC3339Nano", rating)
	}

	// Preview must not modify the card — it should still be in the due list.
	cards := listDueCards(t, svc, "")
	assert.Len(t, cards, 1, "preview should not affect due list")
}

func TestFlashcards_RenderMarkdown(t *testing.T) {
	svc := newTestFlashcardsService(t)

	res, err := svc.HandleWebUIAction("render-markdown", map[string]any{
		"content": "# Hello\n\nWorld",
	})
	require.NoError(t, err)

	m, ok := res.(map[string]any)
	require.True(t, ok)
	html, ok := m["html"].(string)
	require.True(t, ok, "expected html string")
	assert.Contains(t, html, "Hello")
	assert.Contains(t, html, "World")
}

func TestFlashcards_UnknownAction(t *testing.T) {
	svc := newTestFlashcardsService(t)
	_, err := svc.HandleWebUIAction("nonexistent", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}
