package insight

import (
	"net/http"
	"sort"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// HourlySlots is how many clock-hour slots Hourly returns, the newest one
// running to now.
const HourlySlots = 24

// HourlyAPI serves GET /api/metrics/hourly (global boss roles) and, with
// allowed true, GET /api/admin/metrics/hourly.
func (s *Service) HourlyAPI(w http.ResponseWriter, r *http.Request, allowed bool) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !allowed {
		http.Error(w, "metrics are for boss roles (SWITCHBOARD_BOSS_ROLES)", http.StatusForbidden)
		return
	}
	out, err := s.Hourly()
	if err != nil {
		http.Error(w, "query failed", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, out)
}

// Hourly counts the mail, reports and questions created in the last
// HourlySlots clock hours, by hour, and per role over the 60 minutes ending
// now. It only reads. Slots start on whole hours of UTC, which are whole
// local hours in every zone with a whole-hour offset.
func (s *Service) Hourly() (wire.HourlyMetrics, error) {
	to := s.now().UTC()
	from := to.Truncate(time.Hour).Add(-(HourlySlots - 1) * time.Hour)
	lastFrom := to.Add(-time.Hour)
	out := wire.HourlyMetrics{From: from, To: to, Hours: make([]wire.HourCount, HourlySlots), Roles: []wire.RoleHourly{}}
	for i := range out.Hours {
		out.Hours[i].Start = from.Add(time.Duration(i) * time.Hour)
	}
	roles := map[string]*wire.RoleHourly{}
	role := func(name string) *wire.RoleHourly {
		r, ok := roles[name]
		if !ok {
			r = &wire.RoleHourly{Role: name}
			roles[name] = r
		}
		return r
	}
	// slot is the hour slot t falls in, -1 outside the window.
	slot := func(t time.Time) int {
		if t.Before(from) || t.After(to) {
			return -1
		}
		return int(t.Sub(from) / time.Hour)
	}
	recent := func(t time.Time) bool { return !t.Before(lastFrom) && !t.After(to) }
	// created_at is RFC 3339 text whose fraction length varies, so the SQL
	// bound is one second early and the exact cut is made on the parsed time.
	textFrom := from.Add(-time.Second).Format("2006-01-02T15:04:05")

	rows, err := s.db.Query(`SELECT sender, recipient, created_at FROM messages WHERE created_at>=? AND sender<>?`, textFrom, identity.SystemSender)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var sender, recipient, at string
		if err = rows.Scan(&sender, &recipient, &at); err != nil {
			rows.Close()
			return out, err
		}
		t, perr := time.Parse(time.RFC3339Nano, at)
		if perr != nil {
			continue
		}
		if i := slot(t); i >= 0 {
			out.Hours[i].Mail++
		}
		if recent(t) {
			out.LastHour.Mail++
			role(sender).Sent++
			role(recipient).Received++
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}

	rows, err = s.db.Query(`SELECT author, created_at FROM reports WHERE created_at>=?`, textFrom)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var author, at string
		if err = rows.Scan(&author, &at); err != nil {
			rows.Close()
			return out, err
		}
		t, perr := time.Parse(time.RFC3339Nano, at)
		if perr != nil {
			continue
		}
		if i := slot(t); i >= 0 {
			out.Hours[i].Reports++
		}
		if recent(t) {
			out.LastHour.Reports++
			role(author).Reports++
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}

	rows, err = s.db.Query(`SELECT asker, created_at FROM questions WHERE created_at>=?`, ms(from))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var asker string
		var at int64
		if err = rows.Scan(&asker, &at); err != nil {
			rows.Close()
			return out, err
		}
		t := fromMS(at)
		if i := slot(t); i >= 0 {
			out.Hours[i].Questions++
		}
		if recent(t) {
			out.LastHour.Questions++
			role(asker).Questions++
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}

	for _, r := range roles {
		out.Roles = append(out.Roles, *r)
	}
	sort.Slice(out.Roles, func(i, j int) bool {
		a, b := out.Roles[i], out.Roles[j]
		ta, tb := a.Sent+a.Received+a.Reports+a.Questions, b.Sent+b.Received+b.Reports+b.Questions
		if ta != tb {
			return ta > tb
		}
		return a.Role < b.Role
	})
	return out, nil
}
