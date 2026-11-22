package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Proposals (#409). An admin or a global boss lets a role propose tips
// (rule_proposers); a role is never taken for a proposer by its name or
// slot. A proposer's tip on its own service alone is in force at once;
// on tags, on every role or on other services it waits as proposed until
// an admin or a global boss approves it (through the replace flow) or
// rejects it. Decisions stay theirs alone, and a group boss writes nothing.

func (s *Service) isProposer(role string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM rule_proposers WHERE role=?`, role).Scan(&n)
	return n > 0, err
}

// proposal checks a proposer's request and gives the state its row starts
// in, or the reason it is refused. The row belongs to the proposer's group.
func proposal(c Caller, q *wire.RuleRequest) (status, refusal string) {
	if q.Kind != wire.RuleTip {
		return "", "a proposer adds tips; decisions are written by an admin or a global boss"
	}
	if q.NewTopic {
		return "", "a proposer picks a topic (rules topics); an admin or a global boss makes new ones"
	}
	q.Group = c.Group
	if q.Scope.Kind == wire.ScopeService && len(q.Scope.Values) == 1 && q.Scope.Values[0] == ServiceOf(c.Role) {
		return wire.RuleActive, ""
	}
	if len(q.Replace) > 0 || q.Alongside {
		return "", "replace and alongside are chosen when the proposal is approved"
	}
	return wire.RuleProposed, ""
}

// ownServiceTip reports whether o is a tip scoped to role's service alone,
// the only rows a proposer may replace.
func ownServiceTip(o row, role string) bool {
	return o.Kind == wire.RuleTip && o.scope.Kind == wire.ScopeService &&
		len(o.scope.Values) == 1 && o.scope.Values[0] == ServiceOf(role)
}

// checkReplace checks the rows a new row in force replaces: each in force,
// in its group, and for a proposer a tip on its own service alone.
func (s *Service) checkReplace(tx *sql.Tx, ids []int64, group string, c Caller) error {
	for _, id := range ids {
		o, err := s.get(tx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return badRequest(fmt.Sprintf("replace: no rule #%d", id))
		}
		if err != nil {
			return err
		}
		if o.Status != wire.RuleActive {
			return badRequest(fmt.Sprintf("replace: rule #%d is %s, not in force", id, o.Status))
		}
		if o.Group != group {
			return badRequest(fmt.Sprintf("replace: rule #%d is in another group", id))
		}
		if !c.Full && !ownServiceTip(o, c.Role) {
			return badRequest(fmt.Sprintf("replace: rule #%d is not a tip on your own service alone", id))
		}
	}
	return nil
}

// noticeProposal tells the global bosses that rw waits for approval. The
// mail asks no reply; a failure is the hook's to log.
func (s *Service) noticeProposal(rw row) {
	if s.hooks.Notice == nil {
		return
	}
	subject := fmt.Sprintf("[규칙 제안] #%d %s", rw.ID, rw.Topic)
	body := fmt.Sprintf("제안한 역할: %s\n범위: %s\n주제: %s\n\n%s\n\n승인: switchboard rules approve %d\n거절: switchboard rules reject %d --reason …",
		rw.Author, rw.scope.String(), rw.Topic, rw.Text, rw.ID, rw.ID)
	s.hooks.Notice(subject, body, fmt.Sprintf("rule-proposal-%d", rw.ID))
}

// readReview reads an approve or reject body; an empty body is no choice.
func readReview(w http.ResponseWriter, r *http.Request) (wire.RuleReview, bool) {
	var q wire.RuleReview
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&q)
	if err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return q, false
	}
	q.Reason = strings.TrimSpace(q.Reason)
	if len(q.Reason) > wire.MaxRuleText {
		http.Error(w, fmt.Sprintf("reason: up to %d bytes", wire.MaxRuleText), http.StatusBadRequest)
		return q, false
	}
	return q, true
}

// notProposed is the 409 of reviewing a row that is not proposed.
type notProposed struct{ status string }

func (e notProposed) Error() string { return e.status }

// approveAPI is POST /api/rules/{id}/approve: a full caller puts a proposed
// row in force. Rows in force of its group and topic on an overlapping
// scope need replace or alongside, as for an add (409 with them).
func (s *Service) approveAPI(w http.ResponseWriter, r *http.Request, c Caller, id int64) {
	if !c.Full {
		http.Error(w, forbidden, http.StatusForbidden)
		return
	}
	q, ok := readReview(w, r)
	if !ok {
		return
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	err := store.InTx(r.Context(), s.db, func(tx *sql.Tx) error {
		rw, err := s.get(tx, id)
		if err != nil {
			return err
		}
		if rw.Status != wire.RuleProposed {
			return notProposed{rw.Status}
		}
		if err := s.checkReplace(tx, q.Replace, rw.Group, c); err != nil {
			return err
		}
		if !q.Alongside {
			inForce, err := queryRules(tx, `status=? AND grp=? AND topic=?`, wire.RuleActive, rw.Group, rw.Topic)
			if err != nil {
				return err
			}
			if left := leftInForce(inForce, rw.scope, q.Replace, id); len(left) > 0 {
				return conflictError{left}
			}
		}
		if _, err := tx.Exec(`UPDATE rules SET status=?, replaces=?, reviewed_by=?, reviewed_at=? WHERE id=? AND status=?`,
			wire.RuleActive, joinInts(q.Replace), c.Role, now, id, wire.RuleProposed); err != nil {
			return err
		}
		return replaceRows(tx, id, q.Replace)
	})
	s.writeReviewed(w, r, id, err)
}

// rejectAPI is POST /api/rules/{id}/reject: a full caller turns a proposed
// row down, with an optional reason. It stays in the full list (all=1).
func (s *Service) rejectAPI(w http.ResponseWriter, r *http.Request, c Caller, id int64) {
	if !c.Full {
		http.Error(w, forbidden, http.StatusForbidden)
		return
	}
	q, ok := readReview(w, r)
	if !ok {
		return
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	err := store.InTx(r.Context(), s.db, func(tx *sql.Tx) error {
		rw, err := s.get(tx, id)
		if err != nil {
			return err
		}
		if rw.Status != wire.RuleProposed {
			return notProposed{rw.Status}
		}
		_, err = tx.Exec(`UPDATE rules SET status=?, reviewed_by=?, reviewed_at=?, review_note=? WHERE id=? AND status=?`,
			wire.RuleRejected, c.Role, now, q.Reason, id, wire.RuleProposed)
		return err
	})
	s.writeReviewed(w, r, id, err)
}

func (s *Service) writeReviewed(w http.ResponseWriter, r *http.Request, id int64, err error) {
	var conflict conflictError
	var bad badRequest
	var np notProposed
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.NotFound(w, r)
		return
	case errors.As(err, &np):
		http.Error(w, fmt.Sprintf("rule #%d is %s, not proposed", id, np.status), http.StatusConflict)
		return
	case errors.As(err, &conflict):
		writeConflict(w, conflict, true)
		return
	case errors.As(err, &bad):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, "could not review the rule", http.StatusInternalServerError)
		return
	}
	rw, err := s.get(s.db, id)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, rw.Rule)
}

// proposersAPI is /api/rules/proposers for a full caller: GET lists the
// roles that may propose tips; POST {"role","on"} sets one.
func (s *Service) proposersAPI(w http.ResponseWriter, r *http.Request, c Caller) {
	if !c.Full {
		http.Error(w, "proposers are read and set by an admin or a global boss (SWITCHBOARD_BOSS_ROLES)", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		out, err := s.proposers()
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, out)
	case http.MethodPost:
		var q wire.RuleProposer
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&q) != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		q.Role = strings.TrimSpace(q.Role)
		if err := checkRoleName(q.Role); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.setProposer(r.Context(), q.Role, q.On, c.Role); err != nil {
			http.Error(w, "could not set the proposer", http.StatusInternalServerError)
			return
		}
		out, err := s.proposers()
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, out)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) setProposer(ctx context.Context, role string, on bool, by string) error {
	if !on {
		_, err := s.db.ExecContext(ctx, `DELETE FROM rule_proposers WHERE role=?`, role)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO rule_proposers(role,set_by,set_at) VALUES(?,?,?) ON CONFLICT(role) DO NOTHING`,
		role, by, s.now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Service) proposers() (wire.RuleProposers, error) {
	out := wire.RuleProposers{Proposers: []wire.RuleProposer{}}
	rows, err := s.db.Query(`SELECT role,set_by,set_at FROM rule_proposers ORDER BY role`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		p := wire.RuleProposer{On: true}
		var at string
		if err := rows.Scan(&p.Role, &p.SetBy, &at); err != nil {
			return out, err
		}
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
			p.SetAt = &t
		}
		out.Proposers = append(out.Proposers, p)
	}
	return out, rows.Err()
}
