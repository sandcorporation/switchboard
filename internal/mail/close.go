package mail

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// Close serves POST /api/messages/{id}/close (role is the caller, who must
// be the sender) and POST /api/admin/messages/{id}/close (role is empty: an
// admin may close any directive). Closing marks a directive as needing no
// answer: it stores no_reply=1, the flag `send --no-reply` sets, so the
// progress view, the directive reminders and the sweep that sends them
// (all through presence openDirectives) drop it on their next read.
// A directive its recipient has not read yet is withdrawn instead (#354,
// withdraw.go): no_reply=1 is stored as well, and the answer says
// withdrawn. Closing again answers the same. 404 for no such message, 403 for a
// caller that is not the sender, 400 for mail that is not a directive (a
// plain reply, or system mail).
//
// Given the ID of an answer the caller received to a directive it sent,
// Close closes that directive and marks the answer complete for the caller
// in one transaction (#327).
func (s *Service) Close(w http.ResponseWriter, r *http.Request, role string, id int64) {
	m, err := s.Get(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// The sender's close of a directive that has not ended ends it (#470):
	// the closer says what is left. An admin's close (role "") does not ask.
	note, ok := leftNote(w, r)
	if !ok {
		return
	}
	claim := claimOf(note.Left, note.LeftNone)
	// leftCheck refuses what the claim cannot be for directive d; ending
	// reports whether this close ends d.
	leftCheck := func(d int64) (ending, ok bool) {
		ending = role != "" && !s.ended(d)
		if msg := claim.problem(ending, d); msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return false, false
		}
		if len(claim.items) > 0 && !ending {
			http.Error(w, fmt.Sprintf(`--left goes with the close that ends a directive; #%d has ended, so use left add %d "ITEM" (repeatable)`, d, d), http.StatusBadRequest)
			return false, false
		}
		return ending, true
	}
	if d, ok := s.answeredDirective(role, m); ok {
		ending, ok := leftCheck(d)
		if !ok {
			return
		}
		err = store.InTx(r.Context(), s.db, func(tx *sql.Tx) error {
			if _, err := tx.Exec(`UPDATE messages SET no_reply=1 WHERE id=? AND no_reply=0`, d); err != nil {
				return err
			}
			if ending {
				if err := recordLeft(tx, d, role, claim, s.now()); err != nil {
					return err
				}
			}
			return markComplete(tx, role, []int64{id}, s.now().UTC())
		})
		if err != nil {
			http.Error(w, "update failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, wire.Closed{ID: d, Closed: true, Completed: id})
		return
	}
	if role != "" && m.Sender != role {
		http.Error(w, "only the sender may close a directive", http.StatusForbidden)
		return
	}
	if !s.isDirective(m) {
		http.Error(w, "not a directive", http.StatusBadRequest)
		return
	}
	ending, ok := leftCheck(id)
	if !ok {
		return
	}
	var left string
	var withdrawn bool
	err = store.InTx(r.Context(), s.db, func(tx *sql.Tx) error {
		left, withdrawn = "", false
		if _, err := tx.Exec(`UPDATE messages SET no_reply=1 WHERE id=? AND no_reply=0`, id); err != nil {
			return err
		}
		if ending {
			if err := recordLeft(tx, id, role, claim, s.now()); err != nil {
				return err
			}
		}
		var e error
		left, e = withdraw(tx, id, s.now())
		if errors.Is(e, errAlreadyRead) {
			return nil // read: only no_reply, as before
		}
		withdrawn = e == nil
		return e
	})
	if err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	if left != "" {
		s.hub.Publish(left)
	}
	writeJSON(w, wire.Closed{ID: id, Closed: true, Withdrawn: withdrawn})
}

// answeredDirective is the directive m answers when role received m and
// sent that directive: m replies to it and is not a directive itself, and
// neither is system mail.
func (s *Service) answeredDirective(role string, m wire.Message) (int64, bool) {
	if role == "" || m.Recipient != role || m.ReplyTo == 0 || identity.IsSystemSender(m.Sender) || (FlowMail{ReplyTo: m.ReplyTo, Directive: m.Directive}).IsDirective() {
		return 0, false
	}
	d, err := s.Get(m.ReplyTo)
	if err != nil || d.Sender != role || identity.IsSystemSender(d.Sender) || !s.isDirective(d) {
		return 0, false
	}
	return d.ID, true
}
