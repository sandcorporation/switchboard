package requests

import (
	"context"
	"database/sql"
)

// The request chain (#564): which request a mail belongs to, read on every
// read so no stored row changes. A mail belongs to
//
//   - the request it was sent with (request_id, send --request-id);
//   - else, when it replies, the request of the mail it replies to;
//   - else, when it carries a task, the request of an earlier mail with the
//     same task sent to its sender: a coordinator that got a request's mail
//     and sends its worker new mail under that task.
//
// The chain follows down from each linked mail, so a worker's reply to its
// coordinator's mail joins the request too. A mail reached by more than one
// path takes the newest request. A left item belongs to the request it was
// attached to by hand (left_items.request_id), else its directive's.

// ChainSQL opens a WITH clause whose table chain(id,rid) is every mail that
// belongs to a request and that request. A query appends ", ..." or a
// SELECT that joins chain.
const ChainSQL = `WITH RECURSIVE linked(id,rid) AS (
	SELECT id,request_id FROM messages WHERE request_id IS NOT NULL
	UNION
	SELECT m.id,l.rid FROM linked l JOIN messages m ON m.reply_to=l.id WHERE m.request_id IS NULL
	UNION
	SELECT m.id,l.rid FROM linked l JOIN messages p ON p.id=l.id JOIN messages m ON m.task_id=p.task_id AND m.sender=p.recipient AND m.id>p.id
		WHERE p.task_id<>'' AND m.request_id IS NULL AND m.reply_to IS NULL
), chain(id,rid) AS (SELECT id,MAX(rid) FROM linked GROUP BY id)
`

// LeftRequestExpr is the request of left item l (joined with chain c on
// c.id=l.directive_id), 0 for none.
const LeftRequestExpr = `COALESCE(l.request_id,c.rid,0)`

// MailRequests maps every mail that belongs to a request to that request.
func MailRequests(ctx context.Context, db *sql.DB) (map[int64]int64, error) {
	rows, err := db.QueryContext(ctx, ChainSQL+`SELECT id,rid FROM chain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, rid int64
		if err = rows.Scan(&id, &rid); err != nil {
			return nil, err
		}
		out[id] = rid
	}
	return out, rows.Err()
}
