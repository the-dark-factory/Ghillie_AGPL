package diary

import (
	"strings"
	"testing"
	"time"
)

const feed = "BEGIN:VCALENDAR\r\n" +
	"BEGIN:VEVENT\r\n" +
	"SUMMARY:Dentist\\, the good one\r\n" +
	"DTSTART:20260806T140000Z\r\n" +
	"DTEND:20260806T143000Z\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VEVENT\r\n" +
	"SUMMARY:Stand\r\n" +
	" up\r\n" +
	"DTSTART;TZID=Europe/London:20260807T091500\r\n" +
	"RRULE:FREQ=WEEKLY\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VEVENT\r\n" +
	"SUMMARY:All day thing\r\n" +
	"DTSTART;VALUE=DATE:20260808\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VEVENT\r\n" +
	"SUMMARY:No start — skipped, never guessed\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestParseICS(t *testing.T) {
	events, err := ParseICS(strings.NewReader(feed), "gcal")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3 (the startless one is skipped): %+v", len(events), events)
	}
	if events[0].Title != "Dentist, the good one" {
		t.Fatalf("escape handling: %q", events[0].Title)
	}
	if !events[0].Start.Equal(time.Date(2026, 8, 6, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("UTC start wrong: %v", events[0].Start)
	}
	if events[1].Title != "Standup" {
		t.Fatalf("unfolding: %q", events[1].Title)
	}
	if !events[1].Recurring {
		t.Fatal("RRULE must mark Recurring — the surface owes the person that honesty")
	}
	lon, _ := time.LoadLocation("Europe/London")
	if !events[1].Start.Equal(time.Date(2026, 8, 7, 9, 15, 0, 0, lon)) {
		t.Fatalf("TZID start wrong: %v", events[1].Start)
	}
	if events[2].Start.Hour() != 0 {
		t.Fatalf("all-day should land at local midnight: %v", events[2].Start)
	}
	for _, e := range events {
		if e.Source != "feed" || e.Calendar != "gcal" {
			t.Fatalf("source labels wrong: %+v", e)
		}
	}
}
