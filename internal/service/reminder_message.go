package service

import (
	"bytes"
	"html"
	"html/template"
	"strconv"
	"strings"
	"time"
	"unicode"

	"time-leak/internal/domain"
)

type reminderMessage struct{ Subject, Text, HTML string }
type reminderCopy struct{ Subject, Greeting, Lead, DateLabel, Preparation, Open, Settings, Footer string }

var reminderCopies = map[string]reminderCopy{
	"en": {"TimeLeak: {days} {unit} until {title}", "Hello!", "Your important event is in {days} {unit}:", "Event date", "Take a moment to check that everything is ready.", "Open my calendar", "Manage email reminders", "You received this reminder because you enabled email notifications for your TimeLeak calendar."},
	"ru": {"TimeLeak: {title} — через {days} {unit}", "Здравствуйте!", "Через {days} {unit} у вас важное событие:", "Дата события", "Проверьте, всё ли готово к этому дню.", "Открыть мой календарь", "Настроить напоминания", "Вы получили это напоминание, потому что включили письма для своего календаря TimeLeak."},
	"kk": {"TimeLeak: {title} — {days} күн қалды", "Сәлеметсіз бе!", "{days} күннен кейін маңызды оқиғаңыз бар:", "Оқиға күні", "Осы күнге бәрі дайын екенін тексеріңіз.", "Күнтізбемді ашу", "Еске салғыштарды баптау", "Бұл хат TimeLeak күнтізбеңіз үшін email еске салғыштарын қосқаныңызға байланысты жіберілді."},
	"zh": {"TimeLeak：距离{title}还有{days}天", "您好！", "距离您的重要事件还有{days}天：", "事件日期", "请检查您是否已经做好准备。", "打开我的日历", "管理邮件提醒", "您收到此提醒，是因为您已为 TimeLeak 日历启用邮件通知。"},
	"hi": {"TimeLeak: {title} में {days} दिन बाकी", "नमस्ते!", "आपका महत्वपूर्ण कार्यक्रम {days} दिन बाद है:", "कार्यक्रम की तारीख", "जाँच लें कि उस दिन के लिए सब तैयार है।", "मेरा कैलेंडर खोलें", "ईमेल रिमाइंडर प्रबंधित करें", "आपको यह रिमाइंडर इसलिए मिला है क्योंकि आपने अपने TimeLeak कैलेंडर के लिए ईमेल सूचनाएँ सक्षम की हैं।"},
	"fr": {"TimeLeak : {title} dans {days} {unit}", "Bonjour !", "Votre événement important aura lieu dans {days} {unit} :", "Date de l’événement", "Prenez un moment pour vérifier que tout est prêt.", "Ouvrir mon calendrier", "Gérer les rappels par email", "Vous recevez ce rappel car vous avez activé les notifications par email de votre calendrier TimeLeak."},
	"de": {"TimeLeak: {title} in {days} {unit}", "Hallo!", "Ihr wichtiges Ereignis ist in {days} {unit}:", "Datum des Ereignisses", "Prüfen Sie, ob alles für diesen Tag vorbereitet ist.", "Meinen Kalender öffnen", "E-Mail-Erinnerungen verwalten", "Sie erhalten diese Erinnerung, weil Sie E-Mail-Benachrichtigungen für Ihren TimeLeak-Kalender aktiviert haben."},
	"es": {"TimeLeak: {title} en {days} {unit}", "¡Hola!", "Su evento importante será dentro de {days} {unit}:", "Fecha del evento", "Compruebe que todo esté preparado para ese día.", "Abrir mi calendario", "Gestionar recordatorios por correo", "Recibe este recordatorio porque activó las notificaciones por correo de su calendario TimeLeak."},
	"it": {"TimeLeak: {title} tra {days} {unit}", "Ciao!", "Il tuo evento importante è tra {days} {unit}:", "Data dell’evento", "Controlla che tutto sia pronto per quel giorno.", "Apri il mio calendario", "Gestisci i promemoria email", "Ricevi questo promemoria perché hai attivato le notifiche email per il tuo calendario TimeLeak."},
	"pt": {"TimeLeak: {title} em {days} {unit}", "Olá!", "O seu evento importante será em {days} {unit}:", "Data do evento", "Verifique se está tudo preparado para esse dia.", "Abrir o meu calendário", "Gerir lembretes por email", "Recebe este lembrete porque ativou as notificações por email do seu calendário TimeLeak."},
	"ar": {"TimeLeak: {title} — بعد {remaining}", "مرحباً!", "موعد مناسبتك المهمة بعد {remaining}:", "تاريخ المناسبة", "تحقق من أن كل شيء جاهز لهذا اليوم.", "افتح تقويمي", "إدارة تذكيرات البريد الإلكتروني", "تلقيت هذا التذكير لأنك فعّلت إشعارات البريد الإلكتروني لتقويم TimeLeak الخاص بك."},
}

func normalizeReminderLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	language = strings.ReplaceAll(language, "_", "-")
	language = strings.SplitN(language, "-", 2)[0]
	if language == "kz" {
		language = "kk"
	}
	if _, ok := reminderCopies[language]; !ok {
		return "en"
	}
	return language
}
func reminderDayUnit(language string, days int) string {
	switch language {
	case "ru":
		if days%10 == 1 && days%100 != 11 {
			return "день"
		}
		if days%10 >= 2 && days%10 <= 4 && (days%100 < 12 || days%100 > 14) {
			return "дня"
		}
		return "дней"
	case "fr":
		if days == 1 {
			return "jour"
		}
		return "jours"
	case "de":
		if days == 1 {
			return "Tag"
		}
		return "Tagen"
	case "es":
		if days == 1 {
			return "día"
		}
		return "días"
	case "it":
		if days == 1 {
			return "giorno"
		}
		return "giorni"
	case "pt":
		if days == 1 {
			return "dia"
		}
		return "dias"
	default:
		if days == 1 {
			return "day"
		}
		return "days"
	}
}

// The Arabic phrase follows "بعد", so the dual is oblique (يومين) and
// counts above ten take the singular accusative rather than the plural أيام.
func reminderRemainingDays(language string, days int) string {
	if language == "ar" {
		switch {
		case days == 1:
			return "يوم واحد"
		case days == 2:
			return "يومين"
		case days >= 3 && days <= 10:
			return strconv.Itoa(days) + " أيام"
		default:
			return strconv.Itoa(days) + " يومًا"
		}
	}
	return strconv.Itoa(days) + " " + reminderDayUnit(language, days)
}

func reminderDisplayDate(date, language string) string {
	parsed, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	months := map[string][]string{
		"ru": {"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"},
		"kk": {"қаңтар", "ақпан", "наурыз", "сәуір", "мамыр", "маусым", "шілде", "тамыз", "қыркүйек", "қазан", "қараша", "желтоқсан"},
		"en": {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	}
	if localized, ok := months[language]; ok {
		if language == "kk" {
			return strconv.Itoa(parsed.Year()) + " жылғы " + strconv.Itoa(parsed.Day()) + " " + localized[int(parsed.Month())-1]
		}
		return strconv.Itoa(parsed.Day()) + " " + localized[int(parsed.Month())-1] + " " + strconv.Itoa(parsed.Year())
	}
	return date
}

var reminderHTML = template.Must(template.New("reminder").Parse(`<!doctype html><html lang="{{.Language}}" dir="{{.Direction}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="margin:0;background:#fffaf3;font-family:Arial,Helvetica,sans-serif;color:#313131"><table role="presentation" style="width:100%;border-spacing:0"><tr><td style="padding:28px 16px"><table role="presentation" style="max-width:560px;width:100%;margin:auto;border-spacing:0;background:#ffffff;border-radius:18px"><tr><td style="background:#ffedd3;padding:26px;border-radius:18px 18px 0 0;font-size:28px;font-weight:800;color:#cf9443">TimeLeak</td></tr><tr><td style="padding:28px"><p style="font-size:16px">{{.Greeting}}</p><p style="font-size:16px;line-height:1.6">{{.Lead}}</p><h1 style="font-size:24px;line-height:1.4;overflow-wrap:anywhere">{{.Title}}</h1><p style="font-size:16px"><strong>{{.DateLabel}}:</strong> {{.Date}}</p><p style="font-size:15px;line-height:1.6">{{.Preparation}}</p><p style="padding:16px 0"><a href="{{.CalendarURL}}" style="display:inline-block;background:#f8ad3d;color:#313131;text-decoration:none;padding:14px 20px;border-radius:12px;font-weight:700">{{.Open}}</a></p><p style="font-size:12px;color:#454545;line-height:1.6">{{.Footer}}</p><p><a href="{{.SettingsURL}}" style="font-size:13px;color:#9b651d">{{.Settings}}</a></p></td></tr></table></td></tr></table></body></html>`))

func renderReminderMessage(delivery domain.ReminderDelivery, baseURL string) reminderMessage {
	language := normalizeReminderLanguage(delivery.Language)
	copy := reminderCopies[language]
	if baseURL == "" {
		baseURL = "https://timeleak.kz"
	}
	replace := strings.NewReplacer("{days}", strconv.Itoa(delivery.DaysBefore), "{unit}", reminderDayUnit(language, delivery.DaysBefore), "{remaining}", reminderRemainingDays(language, delivery.DaysBefore), "{title}", delivery.Title)
	subject := strings.Join(strings.FieldsFunc(replace.Replace(copy.Subject), func(r rune) bool { return unicode.IsControl(r) }), " ")
	lead := replace.Replace(copy.Lead)
	date := reminderDisplayDate(delivery.OccurrenceDate, language)
	calendarURL := reminderCalendarURL(baseURL, delivery.OccurrenceDate)
	settingsURL := baseURL + "/about#email-reminders"
	text := copy.Greeting + "\n\n" + lead + "\n" + delivery.Title + "\n" + copy.DateLabel + ": " + date + "\n\n" + copy.Preparation + "\n\n" + copy.Open + ": " + calendarURL + "\n\n" + copy.Footer + "\n" + copy.Settings + ": " + settingsURL
	data := struct{ Language, Direction, Greeting, Lead, Title, DateLabel, Date, Preparation, CalendarURL, SettingsURL, Open, Footer, Settings string }{language, "ltr", copy.Greeting, lead, delivery.Title, copy.DateLabel, date, copy.Preparation, calendarURL, settingsURL, copy.Open, copy.Footer, copy.Settings}
	if language == "ar" {
		data.Direction = "rtl"
	}
	var body bytes.Buffer
	if err := reminderHTML.Execute(&body, data); err != nil {
		return reminderMessage{subject, text, "<p>" + html.EscapeString(text) + "</p>"}
	}
	return reminderMessage{subject, text, body.String()}
}
