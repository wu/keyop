// Package flashcards implements a spaced-repetition flashcard service for the keyop web UI.
//
// # Features
//
//   - Flashcards with a question, answer, and free-form tags
//   - SM-2 spaced-repetition algorithm: each review rates the card as
//     "show again", "hard", "correct", or "easy", updating the next due date
//   - Cards are stored in a per-service SQLite database (~/.keyop/sqlite/flashcards.sql)
//   - Web UI tab with a left-side tag navigator and a due-card list
//
// # SM-2 Algorithm
//
// Each card tracks: ease_factor (default 2.5), interval (days, default 0), repetitions.
// Quality ratings map to SM-2 quality scores:
//
//   - show_again → 0 (complete blackout: reset interval to 1 day, repetitions to 0)
//   - hard       → 2 (incorrect but remembered: progress slowly, ease_factor decreases)
//   - correct    → 3 (correct with difficulty: standard SM-2 progression)
//   - easy       → 4 (correct easily: progress faster, ease_factor increases)
//
// Interval progression (SM-2):
//   - repetitions == 0: interval = 1 day
//   - repetitions == 1: interval = 6 days
//   - repetitions >= 2: interval = round(prev_interval * ease_factor)
//
// ease_factor update: ef = max(1.3, ef + 0.1 - (3-q)*(0.08 + (3-q)*0.02))
//
// # Configuration (YAML)
//
//	name: flashcards
//	type: flashcards
//	config:
//	  db_path: ~/.keyop/sqlite/flashcards.sql  # optional; this is the default
package flashcards
