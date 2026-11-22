package agent

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// resetEpoch is the older form "usage limit reached|<unix seconds>".
var resetEpoch = regexp.MustCompile(`\|(\d{10})\b`)

// resetClock is "resets 3pm", "reset at 10:30pm (Asia/Seoul)", "resets Nov
// 21, 5pm", "resets 21:00".
var resetClock = regexp.MustCompile(`(?i)\bresets?\s+(?:at\s+)?(?:([A-Za-z]{3,9})\s+(\d{1,2}),?\s+(?:at\s+)?)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b(?:\s*\(([A-Za-z_+\-/0-9]+)\))?`)

// MatchLimit looks for any of patterns in a screen's rows, joined by spaces
// (so a wrapped message still matches), and takes the lowest match, the
// newest; at is when the screen was read, the reference for a reset time
// without a date. Adapters whose limit message reads this way use it.
func MatchLimit(patterns []*regexp.Regexp, rows []string, at time.Time) (LimitHit, bool) {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		if r = strings.TrimSpace(r); r != "" {
			parts = append(parts, r)
		}
	}
	text := strings.Join(parts, " ")
	var loc []int
	for _, p := range patterns {
		all := p.FindAllStringIndex(text, -1)
		if len(all) > 0 && (loc == nil || all[len(all)-1][0] > loc[0]) {
			loc = all[len(all)-1]
		}
	}
	if loc == nil {
		return LimitHit{}, false
	}
	// The fingerprint ends with the reset clause (or the match), so what
	// the screen shows below the message does not change it.
	window := strings.ToValidUTF8(text[loc[0]:min(len(text), loc[1]+120)], "")
	fp := text[loc[0]:loc[1]]
	for _, re := range []*regexp.Regexp{resetEpoch, resetClock} {
		if r := re.FindStringIndex(window); r != nil {
			fp = window[:r[1]]
			break
		}
	}
	return LimitHit{Text: fp, Reset: ParseReset(fp, at), Count: max(1, strings.Count(text, fp))}, true
}

// ParseReset reads the reset time out of a usage-limit message seen at at:
// the next such clock time after at (in the zone the message names, else
// at's zone), or the unix time of the older "|<seconds>" form. It returns
// zero when the message gives none.
func ParseReset(msg string, at time.Time) time.Time {
	if m := resetEpoch.FindStringSubmatch(msg); m != nil {
		sec, _ := strconv.ParseInt(m[1], 10, 64)
		return time.Unix(sec, 0)
	}
	m := resetClock.FindStringSubmatch(msg)
	if m == nil {
		return time.Time{}
	}
	hour, _ := strconv.Atoi(m[3])
	minute := 0
	if m[4] != "" {
		minute, _ = strconv.Atoi(m[4])
	}
	if m[5] != "" && (hour < 1 || hour > 12) {
		return time.Time{}
	}
	switch strings.ToLower(m[5]) {
	case "am":
		if hour == 12 {
			hour = 0
		}
	case "pm":
		if hour < 12 {
			hour += 12
		}
	}
	if hour > 23 || minute > 59 {
		return time.Time{}
	}
	loc := at.Location()
	if m[6] != "" {
		if l, err := time.LoadLocation(m[6]); err == nil {
			loc = l
		}
	}
	ref := at.In(loc)
	if m[1] != "" {
		month, ok := monthOf(m[1])
		day, _ := strconv.Atoi(m[2])
		if !ok || day < 1 || day > 31 {
			return time.Time{}
		}
		t := time.Date(ref.Year(), month, day, hour, minute, 0, 0, loc)
		if !t.After(at) {
			t = t.AddDate(1, 0, 0)
		}
		return t
	}
	t := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, minute, 0, 0, loc)
	if !t.After(at) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func monthOf(s string) (time.Month, bool) {
	s = strings.ToLower(s)
	if len(s) < 3 {
		return 0, false
	}
	for m := time.January; m <= time.December; m++ {
		if strings.HasPrefix(strings.ToLower(m.String()), s[:3]) {
			return m, true
		}
	}
	return 0, false
}
