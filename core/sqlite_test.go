package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/abs/path.db", "/abs/path.db"},
		{"relative/path.db", "relative/path.db"},
		{"~", home},
		{"~/data/tasks.sql", filepath.Join(home, "data/tasks.sql")},
	}
	for _, c := range cases {
		if got := ExpandHome(c.in); got != c.want {
			t.Errorf("ExpandHome(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSQLiteDSN(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	dsn := SQLiteDSN("~/data/tasks.sql")
	if !strings.HasPrefix(dsn, "file:"+filepath.Join(home, "data/tasks.sql")+"?") {
		t.Errorf("SQLiteDSN did not expand ~: %q", dsn)
	}
	for _, pragma := range []string{
		"_pragma=busy_timeout(5000)",
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
	} {
		if !strings.Contains(dsn, pragma) {
			t.Errorf("SQLiteDSN missing %q: %q", pragma, dsn)
		}
	}
}
