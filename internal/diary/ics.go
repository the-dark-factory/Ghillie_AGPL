// Package diary reads the person's day so ghillie can INFORM OF EVENTS — the
// one thing a huntsman initiates for. Sources are LOCAL-FIRST and open-
// protocol: an ICS feed fetched outward over HTTPS (Google Calendar's
// "secret address" shape — no Google API, no OAuth, consistent with the
// no-Google-cloud policy), and the Apple sources driven by local automation
// (cmd/ghillie-gui). Nothing of the person's leaves the machine; a feed URL
// is a file they placed, 0600, outside any repo.
package diary

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

// Event is one diary entry. Recurring is HONESTY METADATA: this parser does
// not expand recurrence rules, so a feed's recurring events appear only as
// their first occurrence — a surface showing feed events must say so rather
// than quietly showing a hole in the day (the Apple source has no such gap:
// Calendar.app hands over occurrences already expanded).
type Event struct {
	Start     time.Time
	End       time.Time
	Title     string
	Calendar  string
	Source    string // "apple" | "feed"
	Recurring bool   // carries an RRULE this parser did not expand
}

// ParseICS reads an ICS stream into events. It is deliberately small: VEVENT
// blocks, DTSTART/DTEND (UTC, local, TZID or date-only), SUMMARY, RRULE
// presence. Anything unparseable is skipped, never guessed at.
func ParseICS(r io.Reader, calendar string) ([]Event, error) {
	lines, err := unfold(r)
	if err != nil {
		return nil, err
	}
	var events []Event
	var cur *Event
	for _, line := range lines {
		switch {
		case line == "BEGIN:VEVENT":
			cur = &Event{Calendar: calendar, Source: "feed"}
		case line == "END:VEVENT":
			if cur != nil && cur.Title != "" && !cur.Start.IsZero() {
				events = append(events, *cur)
			}
			cur = nil
		case cur == nil:
			// outside a VEVENT — nothing here is ours
		case strings.HasPrefix(line, "SUMMARY"):
			if _, v, ok := icsValue(line); ok {
				cur.Title = unescapeICS(v)
			}
		case strings.HasPrefix(line, "DTSTART"):
			if t, ok := icsTime(line); ok {
				cur.Start = t
			}
		case strings.HasPrefix(line, "DTEND"):
			if t, ok := icsTime(line); ok {
				cur.End = t
			}
		case strings.HasPrefix(line, "RRULE"):
			cur.Recurring = true
		}
	}
	return events, nil
}

// unfold joins ICS continuation lines (a line starting with space/tab
// continues the previous one) and strips CR.
func unfold(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var lines []string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("diary: reading feed: %w", err)
	}
	return lines, nil
}

// icsValue splits "NAME;PARAM=X:value" into name-with-params and value.
func icsValue(line string) (name, value string, ok bool) {
	i := strings.Index(line, ":")
	if i < 0 {
		return "", "", false
	}
	return line[:i], line[i+1:], true
}

// icsTime parses the DTSTART/DTEND shapes this parser admits:
//
//	DTSTART:20260806T090000Z          (UTC)
//	DTSTART:20260806T090000           (floating — read as local)
//	DTSTART;TZID=Europe/London:20260806T090000
//	DTSTART;VALUE=DATE:20260806       (all-day)
func icsTime(line string) (time.Time, bool) {
	name, v, ok := icsValue(line)
	if !ok {
		return time.Time{}, false
	}
	loc := time.Local
	if i := strings.Index(name, "TZID="); i >= 0 {
		tz := name[i+len("TZID="):]
		if j := strings.IndexAny(tz, ";:"); j >= 0 {
			tz = tz[:j]
		}
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	switch {
	case strings.HasSuffix(v, "Z"):
		if t, err := time.Parse("20060102T150405Z", v); err == nil {
			return t.In(time.Local), true
		}
	case strings.Contains(v, "T"):
		if t, err := time.ParseInLocation("20060102T150405", v, loc); err == nil {
			return t.In(time.Local), true
		}
	default:
		if t, err := time.ParseInLocation("20060102", v, loc); err == nil {
			return t, true // all-day: midnight local
		}
	}
	return time.Time{}, false
}

// unescapeICS handles the escapes a SUMMARY may carry.
func unescapeICS(s string) string {
	r := strings.NewReplacer(`\n`, " ", `\,`, ",", `\;`, ";", `\\`, `\`)
	return r.Replace(s)
}
