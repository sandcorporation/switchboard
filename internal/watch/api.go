package watch

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/sandcorporation/switchboard/internal/wire"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// fail answers an error: a refused rule with its reason, never the
// internal error's text.
func fail(w http.ResponseWriter, err error) {
	var inv errInvalid
	switch {
	case errors.As(err, &inv):
		http.Error(w, inv.Error(), http.StatusBadRequest)
	case errors.Is(err, errConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, errNotFound):
		http.Error(w, "규칙 없음", http.StatusNotFound)
	case errors.Is(err, errNoSecrets):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	default:
		log.Printf("watch api: %v", err)
		http.Error(w, "query failed", http.StatusInternalServerError)
	}
}

func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return false
	}
	return true
}

// AdminAPI serves /api/admin/watches (rest is the path after it, without
// the slash) for a signed-in dashboard login:
//
//	GET               the rules with their last run
//	POST              add (WatchRule) → 201 Watch
//	POST test         one fetch of a written rule (WatchTestRequest) → WatchTest
//	PUT {id}          replace (WatchRule) → Watch
//	POST {id}/on|off  turn on or off → Watch
//	DELETE {id}       remove → 204
func (s *Service) AdminAPI(w http.ResponseWriter, r *http.Request, rest string) {
	ctx := r.Context()
	switch {
	case rest == "" && r.Method == http.MethodGet:
		list, err := s.List(ctx)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
		return
	case rest == "" && r.Method == http.MethodPost:
		var in wire.WatchRule
		if !readBody(w, r, &in) {
			return
		}
		out, err := s.Create(ctx, in)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
		return
	case rest == "test" && r.Method == http.MethodPost:
		var in wire.WatchTestRequest
		if !readBody(w, r, &in) {
			return
		}
		out, err := s.Test(ctx, in.ID, in.WatchRule)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	idText, sub, _ := strings.Cut(rest, "/")
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	switch {
	case sub == "" && r.Method == http.MethodPut:
		var in wire.WatchRule
		if !readBody(w, r, &in) {
			return
		}
		out, err := s.Update(ctx, id, in)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	case sub == "" && r.Method == http.MethodDelete:
		if err := s.Delete(ctx, id); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case (sub == "on" || sub == "off") && r.Method == http.MethodPost:
		out, err := s.SetEnabled(ctx, id, sub == "on")
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// API serves GET /api/watches for a role: the summaries of the rules
// visible decides it may see.
func (s *Service) API(w http.ResponseWriter, r *http.Request, visible func(to string) bool) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := s.Summaries(r.Context(), visible)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
