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
		{"2026-11-27", NoteRecurrenceQuarterly, "2026-11-28", "2027-11-27", []string{"2027-02-27", "2027-05-27", "2027-08-27", "2027-11-27"}},
		{"2026-03-31", NoteRecurrenceQuarterly, "2026-04-01", "2027-03-31", []string{"2026-06-30", "2026-09-30", "2026-12-31", "2027-03-31"}},
		{"2026-11-30", NoteRecurrenceQuarterly, "2026-12-01", "2027-11-30", []string{"2027-02-28", "2027-05-30", "2027-08-30", "2027-11-30"}},
		{"2027-11-30", NoteRecurrenceQuarterly, "2028-01-01", "2028-08-30", []string{"2028-02-29", "2028-05-30", "2028-08-30"}},
		{"2026-03-31", NoteRecurrenceQuarterly, "2026-07-01", "2026-12-31", []string{"2026-09-30", "2026-12-31"}},
		{"2026-03-31", NoteRecurrenceQuarterly, "2026-06-30", "2026-06-30", []string{"2026-06-30"}},
		{"2026-03-31", NoteRecurrenceQuarterly, "2026-07-31", "2026-07-31", []string{}},
		{"2026-03-31", NoteRecurrenceQuarterly, "2026-01-01", "2026-03-31", []string{"2026-03-31"}},
		{"2026-11-30", NoteRecurrenceQuarterly, "9999-01-01", "9999-12-31", []string{"9999-02-28", "9999-05-30", "9999-08-30", "9999-11-30"}},
		{"0001-01-01", "FREQ=WEEKLY", "9999-12-20", "9999-12-31", []string{"9999-12-20", "9999-12-27"}},
	}
	for _, tc := range tests {
		t.Run(tc.date+tc.rule+tc.from, func(t *testing.T) {
			note := Note{NoteDate: tc.date, Lifecycle: NoteRecurring, RecurrenceRule: &tc.rule}
			if got := NoteOccurrences(note, tc.from, tc.to); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			if got := HasNoteOccurrence(note, tc.from, tc.to); got != (len(tc.want) > 0) {
				t.Fatalf("HasNoteOccurrence = %v, want %v", got, len(tc.want) > 0)
			}
		})
	}
}

func TestQuarterlyOccurrencePreview(t *testing.T) {
	rule := NoteRecurrenceQuarterly
	for _, tc := range []struct {
		date string
		want []string
	}{
		{"2026-11-30", []string{"2027-02-28", "2027-05-30", "2027-08-30", "2027-11-30"}},
		{"9999-08-31", []string{"9999-11-30"}},
		{"9999-12-31", []string{}},
		{"invalid", []string{}},
	} {
		note := Note{NoteDate: tc.date, Lifecycle: NoteRecurring, RecurrenceRule: &rule}
		if got := NoteOccurrencePreview(note); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got %v want %v", tc.date, got, tc.want)
		}
	}
}

func TestRecurrenceRulesArePreservedExactly(t *testing.T) {
	for _, rule := range []string{"FREQ=YEARLY", "FREQ=MONTHLY", "FREQ=WEEKLY", NoteRecurrenceQuarterly} {
		change, err := ResolveNoteMutation(Note{}, NoteMutation{Lifecycle: StringChange(NoteRecurring), RecurrenceRule: StringChange(rule)})
		if err != nil || change.RecurrenceRule.Value == nil || *change.RecurrenceRule.Value != rule {
			t.Fatalf("%q: change=%+v err=%v", rule, change, err)
		}
	}
	for _, rule := range []string{"FREQ=MONTHLY;INTERVAL=2", "FREQ=MONTHLY;INTERVAL=03", "freq=monthly;interval=3", "INTERVAL=3;FREQ=MONTHLY", "FREQ=MONTHLY;INTERVAL=3 "} {
		if _, err := ResolveNoteMutation(Note{}, NoteMutation{Lifecycle: StringChange(NoteRecurring), RecurrenceRule: StringChange(rule)}); err == nil {
			t.Fatalf("unsupported rule accepted: %q", rule)
		}
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
