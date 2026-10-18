package domain

import (
	"encoding/json"
	"errors"
	"html"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	NoteOneTime   = "one_time"
	NoteRecurring = "recurring"
)

var ErrInvalidNoteFeatures = errors.New("title must contain at most 200 characters; lifecycle must be one_time or recurring; recurring requires FREQ=YEARLY, FREQ=MONTHLY or FREQ=WEEKLY")

// OptionalString distinguishes an omitted update field from an explicit null.
type OptionalString struct {
	Value *string
	Set   bool
}

func (o *OptionalString) UnmarshalJSON(data []byte) error {
	o.Set = true
	return json.Unmarshal(data, &o.Value)
}

func StringChange(value string) OptionalString { return OptionalString{Value: &value, Set: true} }

type NoteMutation struct {
	Title          OptionalString `json:"title"`
	Lifecycle      OptionalString `json:"lifecycle"`
	RecurrenceRule OptionalString `json:"recurrence_rule"`
	TitleAudioURL  OptionalString `json:"-"`
	ID             string         `json:"-"`
}

// NormalizeNoteTitle strips markup rather than interpreting user-supplied HTML.
var noteTitleMarkup = regexp.MustCompile(`(?is)<!--.*?-->|</?[a-z][^>]*>`)

func NormalizeNoteTitle(value string) string {
	return strings.TrimSpace(noteTitleMarkup.ReplaceAllString(html.UnescapeString(value), ""))
}

func ResolveNoteMutation(existing Note, changes NoteMutation) (NoteMutation, error) {
	if changes.Title.Set && changes.Title.Value != nil {
		title := NormalizeNoteTitle(*changes.Title.Value)
		if utf8.RuneCountInString(title) > 200 {
			return changes, ErrInvalidNoteFeatures
		}
		changes.Title.Value = &title
		if title == "" {
			changes.Title.Value = nil
		}
	}
	lifecycle := existing.Lifecycle
	if lifecycle == "" {
		lifecycle = NoteOneTime
	}
	if changes.Lifecycle.Set {
		if changes.Lifecycle.Value == nil {
			return changes, ErrInvalidNoteFeatures
		}
		lifecycle = *changes.Lifecycle.Value
	}
	rule := existing.RecurrenceRule
	if changes.RecurrenceRule.Set {
		rule = changes.RecurrenceRule.Value
	}
	switch lifecycle {
	case NoteOneTime:
		if changes.RecurrenceRule.Set && rule != nil && *rule != "" {
			return changes, ErrInvalidNoteFeatures
		}
		rule = nil
	case NoteRecurring:
		if rule == nil || (*rule != "FREQ=YEARLY" && *rule != "FREQ=MONTHLY" && *rule != "FREQ=WEEKLY") {
			return changes, ErrInvalidNoteFeatures
		}
	default:
		return changes, ErrInvalidNoteFeatures
	}
	changes.Lifecycle = StringChange(lifecycle)
	changes.RecurrenceRule = OptionalString{Value: rule, Set: true}
	return changes, nil
}

func DefaultNoteTitle(noteType, date string) string {
	title := NormalizeNoteTitle(noteType)
	if title == "" {
		title = "Заметка"
	}
	// Leave room for the date even when an old note_type contains a long label.
	runes := []rune(title)
	if len(runes) > 187 {
		title = string(runes[:187])
	}
	return title + " · " + date
}

func NoteOccurrences(note Note, from, to string) []string {
	return noteOccurrences(note, from, to, 0)
}

func HasNoteOccurrence(note Note, from, to string) bool {
	return len(noteOccurrences(note, from, to, 1)) > 0
}

func noteOccurrences(note Note, from, to string, limit int) []string {
	result := make([]string, 0)
	if note.Lifecycle != NoteRecurring || note.RecurrenceRule == nil || !ValidNoteDate(note.NoteDate) || !ValidNoteDate(from) || !ValidNoteDate(to) || from > to || note.NoteDate > to {
		return result
	}
	start, _ := time.Parse(time.DateOnly, note.NoteDate)
	lo, _ := time.Parse(time.DateOnly, from)
	hi, _ := time.Parse(time.DateOnly, to)
	if lo.Before(start) {
		lo = start
	}
	clamp := func(year int, month time.Month) time.Time {
		day := start.Day()
		last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
		if day > last {
			day = last
		}
		return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}
	switch *note.RecurrenceRule {
	case "FREQ=WEEKLY":
		// Sub saturates for very distant dates; Unix seconds cover years 1..9999.
		days := int((lo.Unix() - start.Unix()) / 86400)
		next := start.AddDate(0, 0, ((days+6)/7)*7)
		for !next.After(hi) {
			result = append(result, next.Format(time.DateOnly))
			if limit > 0 && len(result) >= limit {
				return result
			}
			next = next.AddDate(0, 0, 7)
		}
	case "FREQ=MONTHLY":
		for y, m := lo.Year(), lo.Month(); y <= hi.Year(); {
			next := clamp(y, m)
			if next.After(hi) {
				break
			}
			if !next.Before(lo) {
				result = append(result, next.Format(time.DateOnly))
				if limit > 0 && len(result) >= limit {
					return result
				}
			}
			m++
			if m > 12 {
				y++
				m = 1
			}
		}
	case "FREQ=YEARLY":
		for year := lo.Year(); year <= hi.Year(); year++ {
			next := clamp(year, start.Month())
			if !next.Before(lo) && !next.After(hi) {
				result = append(result, next.Format(time.DateOnly))
				if limit > 0 && len(result) >= limit {
					return result
				}
			}
		}
	}
	return result
}

type NoteShare struct {
	ID         string
	NoteID     string
	Token      string
	CreatedBy  string
	ExpiresAt  time.Time
	ViewsCount int
	SharedBy   string
}
