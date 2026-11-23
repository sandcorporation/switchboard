package search

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// API serves GET /api/search and /api/admin/search:
//
//	?q=PATTERN [&regex=1] [&case=1] [&kind=mail|report|question|left]…
//	[&role=R] [&thread=N] [&since=T] [&until=T] [&limit=N] [&after=CURSOR]
//
// since and until take RFC 3339 or a date (YYYY-MM-DD, UTC; until is then
// the end of that day). see and scope come from the caller's view.
func (sv *Service) API(w http.ResponseWriter, r *http.Request, see See, scope string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q, err := ParseQuery(r.URL.Query())
	if err == nil {
		var res any
		res, err = sv.Search(r.Context(), q, see, scope)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(res)
			return
		}
	}
	if errors.Is(err, ErrBadQuery) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Error(w, "search failed", http.StatusInternalServerError)
}

// ParseQuery reads a Query from URL values (the API's parameters).
func ParseQuery(v url.Values) (Query, error) {
	q := Query{Pattern: v.Get("q"), Regex: flag(v.Get("regex")), Case: flag(v.Get("case")), Kinds: v["kind"], Role: v.Get("role"), After: v.Get("after")}
	var err error
	if s := v.Get("thread"); s != "" {
		if q.Thread, err = strconv.ParseInt(s, 10, 64); err != nil || q.Thread < 1 {
			return q, fmt.Errorf("%w: thread: a message number", ErrBadQuery)
		}
	}
	if s := v.Get("limit"); s != "" {
		if q.Limit, err = strconv.Atoi(s); err != nil || q.Limit < 1 {
			return q, fmt.Errorf("%w: limit: a positive number", ErrBadQuery)
		}
	}
	if q.Since, err = ParseTime(v.Get("since"), false); err != nil {
		return q, err
	}
	if q.Until, err = ParseTime(v.Get("until"), true); err != nil {
		return q, err
	}
	return q, nil
}

func flag(s string) bool { return s == "1" || s == "true" || s == "on" }

// ParseTime reads RFC 3339 or a date YYYY-MM-DD (UTC). A date as an end
// bound is the end of that day. "" is the zero time.
func ParseTime(s string, end bool) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return t, fmt.Errorf("%w: time: RFC 3339 or YYYY-MM-DD", ErrBadQuery)
	}
	if end {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}
