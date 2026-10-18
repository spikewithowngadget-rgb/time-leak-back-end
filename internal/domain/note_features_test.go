package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestNoteOccurrencesCalendarBoundaries(t *testing.T) {
	tests := []struct {
		date, rule, from, to string
		want                 []string
	}{
		{"2024-02-29", "FREQ=YEARLY", "2025-01-01", "2028-12-31", []string{"2025-02-28", "2026-02-28", "2027-02-28", "2028-02-29"}},
		{"2026-01-31", "FREQ=MONTHLY", "2026-02-01", "2026-05-31", []string{"2026-02-28", "2026-03-31", "2026-04-30", "2026-05-31"}},
		{"2026-10-18", "FREQ=WEEKLY", "2026-10-19", "2026-11-02", []string{"2026-10-25", "2026-11-01"}},
		{"2026-10-18", "FREQ=WEEKLY", "2026-10-01", "2026-10-18", []string{"2026-10-18"}},
		{"2027-03-15", "FREQ=YEARLY", "2026-01-01", "2026-12-31", []string{}},
		{"2026-01-31", "FREQ=MONTHLY", "2026-02-27", "2026-02-28", []string{"2026-02-28"}},
		{"2026-01-31", "FREQ=MONTHLY", "2026-03-01", "2026-03-30", []string{}},
		{"0001-01-01", "FREQ=WEEKLY", "9999-12-20", "9999-12-31", []string{"9999-12-20", "9999-12-27"}},
	}
	for _, tc := range tests {
		t.Run(tc.date+tc.rule+tc.from, func(t *testing.T) {
			note := Note{NoteDate: tc.date, Lifecycle: NoteRecurring, RecurrenceRule: &tc.rule}
			if got := NoteOccurrences(note, tc.from, tc.to); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestNoteTitleNormalizationAndLifecycle(t *testing.T) {
	change, err := ResolveNoteMutation(Note{}, NoteMutation{Title: StringChange("  <b>Мама 🎂</b> &amp; семья  ")})
	if err != nil || change.Title.Value == nil || *change.Title.Value != "Мама 🎂 & семья" {
		t.Fatalf("normalization: %+v %v", change, err)
	}
	for _, value := range []string{"", "  ", "<br>"} {
		change, err := ResolveNoteMutation(Note{}, NoteMutation{Title: StringChange(value)})
		if err != nil || change.Title.Value != nil {
			t.Fatal("blank title must reset to default")
		}
	}
	if _, err := ResolveNoteMutation(Note{}, NoteMutation{Title: StringChange(strings.Repeat("я", 201))}); err == nil {
		t.Fatal("long title accepted")
	}
	if _, err := ResolveNoteMutation(Note{}, NoteMutation{Title: StringChange(strings.Repeat("🎂", 200))}); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []NoteMutation{
		{Lifecycle: StringChange("other")},
		{Lifecycle: StringChange(NoteRecurring)},
		{Lifecycle: StringChange(NoteRecurring), RecurrenceRule: StringChange("FREQ=DAILY")},
		{RecurrenceRule: StringChange("FREQ=YEARLY")},
	} {
		if _, err := ResolveNoteMutation(Note{}, mutation); err == nil {
			t.Fatal("invalid lifecycle accepted")
		}
	}
}
