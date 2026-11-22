package handler

import (
	"net/http"
	"reflect"
	"testing"

	"time-leak/internal/domain"
)

func TestQuarterlyNotesCreateUpdateAndCalendar(t *testing.T) {
	for _, encoding := range []string{"json", "multipart"} {
		for _, tc := range []struct {
			date, offDate string
			want          []string
		}{
			{"2026-11-27", "2027-03-27", []string{"2027-02-27", "2027-05-27", "2027-08-27", "2027-11-27"}},
			{"2026-03-31", "2026-07-31", []string{"2026-06-30", "2026-09-30", "2026-12-31", "2027-03-31"}},
			{"2026-11-30", "2027-03-30", []string{"2027-02-28", "2027-05-30", "2027-08-30", "2027-11-30"}},
		} {
			t.Run(encoding+"/"+tc.date, func(t *testing.T) {
				f := newNotesFixture(t)
				mutate := func(method, path string, fields map[string]string, status int) domain.Note {
					t.Helper()
					var resp *http.Response
					if encoding == "multipart" {
						resp = doMultipartReqAuth(t, f.srv.URL, method, path, fields, nil, f.tokens[0])
					} else {
						resp = doReqAuth(t, f.srv.URL, method, path, fields, f.tokens[0])
					}
					requireNoteStatus(t, resp, status)
					var note domain.Note
					decodeJSON(t, resp, &note)
					return note
				}
				assertRule := func(note domain.Note) {
					t.Helper()
					if note.RecurrenceRule == nil || *note.RecurrenceRule != domain.NoteRecurrenceQuarterly || note.Lifecycle != domain.NoteRecurring || note.NoteDate != tc.date {
						t.Fatalf("quarterly note changed: %+v", note)
					}
				}
				note := mutate(http.MethodPost, "/api/v1/auth/notes", map[string]string{
					"note_type": "quarterly", "note_date": tc.date,
					"lifecycle": domain.NoteRecurring, "recurrence_rule": domain.NoteRecurrenceQuarterly,
				}, http.StatusCreated)
				assertRule(note)
				endpoint := "/api/v1/auth/notes/" + note.ID
				mutate(http.MethodPut, endpoint, map[string]string{"lifecycle": domain.NoteOneTime}, http.StatusOK)
				assertRule(mutate(http.MethodPut, endpoint, map[string]string{
					"lifecycle": domain.NoteRecurring, "recurrence_rule": domain.NoteRecurrenceQuarterly,
				}, http.StatusOK))
				assertRule(mutate(http.MethodPut, endpoint, map[string]string{"title": "Квартальный отчёт"}, http.StatusOK))

				resp := doReqAuth(t, f.srv.URL, http.MethodGet, endpoint, nil, f.tokens[0])
				requireNoteStatus(t, resp, http.StatusOK)
				var fetched domain.Note
				decodeJSON(t, resp, &fetched)
				assertRule(fetched)
				if fetched.ID != note.ID || !reflect.DeepEqual(fetched.Occurrences, tc.want) {
					t.Fatalf("single note occurrences: %+v, want %v", fetched, tc.want)
				}

				for _, fields := range []string{"", "&fields=meta"} {
					path := "/api/v1/auth/notes?from=" + tc.want[0] + "&to=" + tc.want[len(tc.want)-1] + fields
					resp = doReqAuth(t, f.srv.URL, http.MethodGet, path, nil, f.tokens[0])
					requireNoteStatus(t, resp, http.StatusOK)
					var page struct {
						Data  []domain.Note `json:"data"`
						Total int           `json:"total"`
					}
					decodeJSON(t, resp, &page)
					if page.Total != 1 || len(page.Data) != 1 || page.Data[0].ID != note.ID || !reflect.DeepEqual(page.Data[0].Occurrences, tc.want) {
						t.Fatalf("range %s: %+v", fields, page)
					}
					assertRule(page.Data[0])
				}
				for _, date := range append(append([]string{}, tc.want...), tc.offDate) {
					resp = doReqAuth(t, f.srv.URL, http.MethodGet, "/api/v1/auth/notes/by-date?date="+date, nil, f.tokens[0])
					requireNoteStatus(t, resp, http.StatusOK)
					var day struct {
						Data  []domain.Note `json:"data"`
						Total int           `json:"total"`
					}
					decodeJSON(t, resp, &day)
					if date == tc.offDate {
						if day.Total != 0 || len(day.Data) != 0 {
							t.Fatalf("note leaked into non-quarterly day: %+v", day)
						}
					} else if day.Total != 1 || len(day.Data) != 1 || day.Data[0].ID != note.ID || !reflect.DeepEqual(day.Data[0].Occurrences, []string{date}) {
						t.Fatalf("day %s: %+v", date, day)
					}
				}
			})
		}
	}
}
