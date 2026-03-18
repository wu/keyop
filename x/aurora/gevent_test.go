package aurora

import (
	"testing"
	"time"
)

func TestExtractGEvents(t *testing.T) {
	// Create a mock ParsedForecast with some G events
	kp5 := 5.0
	kp3 := 3.0
	kp2 := 2.0
	kp4 := 4.0
	kp6 := 6.0
	kp7 := 7.0

	pf := &ParsedForecast{
		Days:    []string{"Mar 18", "Mar 19", "Mar 20"},
		Periods: []string{"00-03UT", "03-06UT", "06-09UT"},
		Table: map[string][]KpEntry{
			"00-03UT": {
				{KP: &kp5, G: "G4", Raw: "5 (G4)"},
				{KP: &kp3, Raw: "3"},
				{KP: &kp2, Raw: "2"},
			},
			"03-06UT": {
				{KP: &kp4, G: "G3", Raw: "4 (G3)"},
				{KP: &kp6, G: "G5", Raw: "6 (G5)"},
				{KP: &kp3, Raw: "3"},
			},
			"06-09UT": {
				{KP: &kp2, Raw: "2"},
				{KP: &kp2, Raw: "2"},
				{KP: &kp7, G: "G6", Raw: "7 (G6)"},
			},
		},
	}

	events, err := extractGEvents(pf)
	if err != nil {
		t.Fatalf("extractGEvents failed: %v", err)
	}

	// Should extract 3 events > G3: G4, G5, G6
	if len(events) != 3 {
		t.Errorf("expected 3 G events, got %d", len(events))
	}

	// Check that G3 is not included
	for _, e := range events {
		if e.GValue <= 3 {
			t.Errorf("G event with value %d (<=3) was included, should be filtered", e.GValue)
		}
	}

	// Check that highest event is G6
	highest := findHighestGEvent(events)
	if highest == nil || highest.GValue != 6 {
		t.Errorf("expected highest G event to be G6, got %v", highest)
	}
}

func TestFilterNewEvents(t *testing.T) {
	now := time.Now()
	e1 := GEvent{GScale: "G4", GValue: 4, StartTime: now, EndTime: now.Add(3 * time.Hour)}
	e2 := GEvent{GScale: "G5", GValue: 5, StartTime: now.Add(24 * time.Hour), EndTime: now.Add(27 * time.Hour)}
	e3 := GEvent{GScale: "G6", GValue: 6, StartTime: now.Add(48 * time.Hour), EndTime: now.Add(51 * time.Hour)}

	current := []GEvent{e1, e2, e3}
	previous := []GEvent{e1}

	newEvents := filterNewEvents(current, previous)
	if len(newEvents) != 2 {
		t.Errorf("expected 2 new events, got %d", len(newEvents))
	}

	// Verify e2 and e3 are in newEvents
	found := make(map[int]bool)
	for _, e := range newEvents {
		found[e.GValue] = true
	}
	if !found[5] || !found[6] {
		t.Errorf("expected to find G5 and G6 in new events")
	}
}
