package mail

import (
	"database/sql"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// A report is a send that answers an open directive without being a reply: a
// worker's "PR opened" notification goes to the role that gave the work under
// the same task id, but as a new send. It is not a directive (#480): it
// leaves no ledger entry to settle, completing it needs no --left, and the
// progress view does not count it. The rule is shared by the ledger and the
// progress view through IsReport.

// IsReport reports whether m, a send, is a report: its recipient sent a
// directive (not itself a report) to m's sender's base under m's non-empty
// task id before m, and that directive has not ended. Replies and system mail
// are never reports. base maps a role to its base role.
func IsReport(db *sql.DB, base func(string) string, m FlowMail) bool {
	if m.TaskID == "" || m.ReplyTo != 0 || identity.IsSystemSender(m.Sender) {
		return false
	}
	rows, err := db.Query(`SELECT m.sender,m.recipient,COALESCE(m.reply_to,0),m.directive,m.no_reply,`+StatusExpr+` FROM messages m `+StatusJoin+` WHERE m.task_id=? AND m.id<? AND m.sender<>?`, m.TaskID, m.ID, identity.SystemSender)
	if err != nil {
		return false
	}
	type cand struct{ sender, recipient string }
	var cands []cand
	for rows.Next() {
		var sender, recipient, status string
		var replyTo int64
		var directive, noReply bool
		if rows.Scan(&sender, &recipient, &replyTo, &directive, &noReply, &status) != nil {
			continue
		}
		if replyTo != 0 && !directive || noReply || IsCompleteStatus(status) {
			continue
		}
		cands = append(cands, cand{sender, recipient})
	}
	rows.Close() // base reads the database: the one connection must be free
	from, to := base(m.Recipient), base(m.Sender)
	for _, c := range cands {
		if base(c.sender) == from && base(c.recipient) == to {
			return true
		}
	}
	return false
}

// isDirective: mail that is a directive (IsDirective), not system mail and
// not a report of an open directive (#480).
func (s *Service) isDirective(m wire.Message) bool {
	if !isDirectiveMail(m) {
		return false
	}
	return !IsReport(s.db, s.base, FlowMail{ID: m.ID, Sender: m.Sender, Recipient: m.Recipient, TaskID: m.TaskID, ReplyTo: m.ReplyTo, Directive: m.Directive})
}
