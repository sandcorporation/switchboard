package rules

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// The session side of the ledger (#408 T2): the rows a role's session
// hook matches tool calls against, and the count of their hits.

// maxHitIDs is the most rows one hits call counts.
const maxHitIDs = 100

// inForceFor is every row in force that reaches role (its group, its
// service, its tags) and is not enforced by a device, newest first. It is
// the same for a full caller: a session gets its own role's rows.
func (s *Service) inForceFor(role string) ([]row, error) {
	group := ""
	if s.hooks.GroupOf != nil {
		group = s.hooks.GroupOf(role)
	}
	tags, err := s.tagsOf(s.db, role)
	if err != nil {
		return nil, err
	}
	rd := reader{group: group, service: ServiceOf(role), tags: tags}
	rows, err := queryRules(s.db, `status=? AND device=''`, wire.RuleActive)
	if err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, rw := range rows {
		if rd.sees(rw) {
			out = append(out, rw)
		}
	}
	return out, nil
}

// Hints is the role's rows for its session hook.
func (s *Service) Hints(role string) (wire.RuleHints, error) {
	rows, err := s.inForceFor(role)
	if err != nil {
		return wire.RuleHints{}, err
	}
	out := wire.RuleHints{Rules: []wire.RuleHint{}}
	for _, rw := range rows {
		out.Rules = append(out.Rules, wire.RuleHint{ID: rw.ID, Kind: rw.Kind, Topic: rw.Topic, Summary: wire.FirstLine(rw.Text), Match: rw.Match})
	}
	return out, nil
}

// mineAPI is GET /api/rules/mine: Hints for the caller, with an ETag over
// the body; a matching If-None-Match is 304.
func (s *Service) mineAPI(w http.ResponseWriter, r *http.Request, c Caller) {
	h, err := s.Hints(c.Role)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	b, _ := json.Marshal(h)
	sum := sha256.Sum256(b)
	tag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if etagMatch(r.Header.Get("If-None-Match"), tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(b, '\n'))
}

// etagMatch reports whether an If-None-Match header names tag (or is *).
func etagMatch(header, tag string) bool {
	for _, v := range splitTrim(header) {
		if v == "*" || v == tag || v == "W/"+tag {
			return true
		}
	}
	return false
}

func splitTrim(v string) []string {
	var out []string
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// hitsAPI is POST /api/rules/hits: counts one hit for each named row among
// the caller's hint rows; other numbers and repeats are left out.
func (s *Service) hitsAPI(w http.ResponseWriter, r *http.Request, c Caller) {
	var q wire.RuleHitsRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&q) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if len(q.IDs) > maxHitIDs {
		http.Error(w, "ids: up to 100", http.StatusBadRequest)
		return
	}
	rows, err := s.inForceFor(c.Role)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	mine := map[int64]bool{}
	for _, rw := range rows {
		mine[rw.ID] = true
	}
	out := wire.RuleHitsResult{Counted: []int64{}}
	now := s.now().UTC().Format(time.RFC3339Nano)
	for _, id := range q.IDs {
		if !mine[id] || slices.Contains(out.Counted, id) {
			continue
		}
		if _, err := s.db.Exec(`INSERT INTO rule_hits(rule_id,hits,last_at) VALUES(?,1,?)
			ON CONFLICT(rule_id) DO UPDATE SET hits=hits+1, last_at=excluded.last_at`, id, now); err != nil {
			http.Error(w, "could not count the hits", http.StatusInternalServerError)
			return
		}
		out.Counted = append(out.Counted, id)
	}
	writeJSON(w, out)
}

// hitCount is a row's hits and last hit.
type hitCount struct {
	hits int64
	last *time.Time
}

// hitCounts reads every row's hit count, for full readers.
func (s *Service) hitCounts() (map[int64]hitCount, error) {
	rows, err := s.db.Query(`SELECT rule_id,hits,last_at FROM rule_hits`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]hitCount{}
	for rows.Next() {
		var (
			id, n int64
			last  sql.NullString
		)
		if err := rows.Scan(&id, &n, &last); err != nil {
			return nil, err
		}
		hc := hitCount{hits: n}
		if t, err := time.Parse(time.RFC3339Nano, last.String); err == nil {
			hc.last = &t
		}
		out[id] = hc
	}
	return out, rows.Err()
}

// withHits fills a full reader's rows with their hit counts.
func withHits(r wire.Rule, hc map[int64]hitCount) wire.Rule {
	if h, ok := hc[r.ID]; ok {
		r.Hits, r.LastHitAt = h.hits, h.last
	}
	return r
}
