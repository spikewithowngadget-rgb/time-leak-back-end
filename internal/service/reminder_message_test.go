package service

import (
	"strings"
	"testing"
	"time-leak/internal/domain"
)

func TestReminderMessageLanguagesAndDays(t *testing.T) {
	for _, language := range []string{"kk", "ru", "en", "zh", "hi", "fr", "de", "es", "it", "pt", "ar"} {
		for _, days := range []int{1, 2, 3, 7, 14, 30} {
			t.Run(language, func(t *testing.T) {
				d := domain.ReminderDelivery{Title: "Birthday & family", Language: language, OccurrenceDate: "2026-11-07", DaysBefore: days}
				m := renderReminderMessage(d, "https://timeleak.kz")
				if m.Subject == "" || m.Text == "" || m.HTML == "" || strings.Contains(m.Text, "{days}") || !strings.Contains(m.Text, d.Title) || !strings.Contains(m.Text, "https://timeleak.kz/?date=2026-11-07") || !strings.Contains(m.HTML, `lang="`+language+`"`) {
					t.Fatalf("bad%smessage:%+v", language, m)
				}
				if language == "ar" && !strings.Contains(m.HTML, `dir="rtl"`) {
					t.Fatal("Arabicdirection")
				}
			})
		}
	}
	if normalizeReminderLanguage("kz-KZ") != "kk" || normalizeReminderLanguage("ru_RU") != "ru" || normalizeReminderLanguage("unknown") != "en" {
		t.Fatal("languagealiases")
	}
	for _, test := range []struct {
		n    int
		word string
	}{{1, "день"}, {2, "дня"}, {3, "дня"}, {5, "дней"}, {11, "дней"}, {21, "день"}, {22, "дня"}, {30, "дней"}} {
		if reminderDayUnit("ru", test.n) != test.word {
			t.Fatalf("plural%d", test.n)
		}
	}
}
func TestReminderMessageEscapesNoteContentAndNoHeaderInjection(t *testing.T) {
	d := domain.ReminderDelivery{Title: `<script>alert('private')</script> & "title"` + "\r\nBcc: unrelated@example.com", Language: "ru", OccurrenceDate: "2026-11-07", DaysBefore: 3}
	m := renderReminderMessage(d, "https://timeleak.kz")
	if strings.Contains(m.HTML, "<script>") || strings.ContainsAny(m.Subject, "\r\n") || !strings.Contains(m.HTML, "&lt;script&gt;") {
		t.Fatal("unsafeHTMLorheaders")
	}
	if !strings.Contains(m.Text, d.Title) || strings.Contains(m.HTML, "javascript:") {
		t.Fatal("titlewaslost")
	}
	if reminderDisplayDate("2026-11-07", "ru") != "7 ноября 2026" || reminderDisplayDate("2026-11-07", "kk") != "2026 жылғы 7 қараша" {
		t.Fatal("localizeddate")
	}
}

func TestReminderMessageArabicRemainingDays(t *testing.T) {
	for _, test := range []struct {
		days   int
		phrase string
	}{
		{1, "يوم واحد"},
		{2, "يومين"},
		{3, "3 أيام"},
		{11, "11 يومًا"},
		{30, "30 يومًا"},
	} {
		delivery := domain.ReminderDelivery{Title: "عيد ميلاد صديق", Language: "ar", OccurrenceDate: "2026-11-07", DaysBefore: test.days}
		message := renderReminderMessage(delivery, "https://timeleak.kz")
		if message.Subject != "TimeLeak: "+delivery.Title+" — بعد "+test.phrase || !strings.Contains(message.Text, "موعد مناسبتك المهمة بعد "+test.phrase+":") {
			t.Errorf("Arabic %d-day phrase: subject=%q text=%q", test.days, message.Subject, message.Text)
		}
		if !strings.Contains(message.HTML, `dir="rtl"`) || !strings.Contains(message.HTML, test.phrase) || strings.Contains(message.Text, "{remaining}") {
			t.Errorf("Arabic %d-day HTML/template interpolation failed", test.days)
		}
	}
}
