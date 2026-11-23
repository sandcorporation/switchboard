package mail

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/sandcorporation/switchboard/internal/identity"
	"github.com/sandcorporation/switchboard/internal/store"
)

// DefaultAlertWindow is how long an alert folds repeats of itself into its
// mail when Alert.Window is zero (#525).
const DefaultAlertWindow = 10 * time.Minute

// Alert is one alert mail from the system sender (#525): a hook or a watch
// telling a role something happened.
type Alert struct {
	To      string        // the receiving mailbox (role)
	Source  string        // where it comes from: hook:<name>, watch:<name>
	Subject string        // with Source, what makes two alerts the same
	Body    string        // the first alert's body stays; repeats do not change it
	Urgent  bool          // urgent priority (delivered with its body, once)
	Window  time.Duration // repeats inside this time from the mail fold into it; 0: DefaultAlertWindow
}

// AlertResult is what SendAlert did.
type AlertResult struct {
	ID    int64 // the mail: the new one, or the earlier one the alert folded into
	New   bool  // a mail was stored; false: the alert was counted on mail ID
	Count int   // how many alerts mail ID stands for now (1 for a new one)
}

var errAlertFields = errors.New("alert needs a recipient, a source and a subject")

// InsertAlert stores a inside tx. When mail to the same mailbox from the
// same source with the same subject was stored less than the window before
// now, no mail is made: that mail's repeat count goes up by one and its last
// time becomes now (alert_repeats, alert_last_at). Its ID, status and
// priority stay, so nothing is delivered again — the event stream and long
// poll emit only new IDs, and an urgent repeat does not ring a second time.
// After the window a new mail starts the next window.
func InsertAlert(tx *sql.Tx, a Alert, now time.Time) (AlertResult, error) {
	if a.To == "" || a.Source == "" || a.Subject == "" {
		return AlertResult{}, errAlertFields
	}
	window := a.Window
	if window <= 0 {
		window = DefaultAlertWindow
	}
	var id int64
	var created string
	var repeats int
	err := tx.QueryRow(`SELECT id,created_at,alert_repeats FROM messages WHERE recipient=? AND alert_source=? AND subject=? ORDER BY id DESC LIMIT 1`,
		a.To, a.Source, a.Subject).Scan(&id, &created, &repeats)
	switch {
	case err == nil:
		if t, perr := time.Parse(time.RFC3339Nano, created); perr == nil && now.Sub(t) < window {
			if _, err = tx.Exec(`UPDATE messages SET alert_repeats=alert_repeats+1, alert_last_at=? WHERE id=?`, now.UTC().Format(time.RFC3339Nano), id); err != nil {
				return AlertResult{}, err
			}
			return AlertResult{ID: id, Count: repeats + 2}, nil
		}
	case !errors.Is(err, sql.ErrNoRows):
		return AlertResult{}, err
	}
	priority := "normal"
	if a.Urgent {
		priority = "urgent"
	}
	res, err := tx.Exec(`INSERT INTO messages(sender,recipient,subject,body,created_at,priority,status,alert_source) VALUES(?,?,?,?,?,?,'pending',?)`,
		identity.SystemSender, a.To, a.Subject, a.Body, now.UTC().Format(time.RFC3339Nano), priority, a.Source)
	if err != nil {
		return AlertResult{}, err
	}
	id, err = res.LastInsertId()
	return AlertResult{ID: id, New: true, Count: 1}, err
}

// SendAlert stores a (InsertAlert) in its own transaction and wakes the
// recipient's event streams when a new mail was stored; a folded repeat wakes
// nothing. The caller tells live dashboards ("mail") either way, since the
// count shows there.
func (s *Service) SendAlert(ctx context.Context, a Alert) (AlertResult, error) {
	var r AlertResult
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		r, err = InsertAlert(tx, a, s.now())
		return err
	})
	if err == nil && r.New && s.hub != nil {
		s.hub.Publish(a.To)
	}
	return r, err
}
