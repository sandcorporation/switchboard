package rules

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// Answer classes (#409). Every answer a person gives to a panel question
// (in the dashboard or a session's panel) goes into rule_classes
// unclassified, inside asks' answer transaction (RecordAnswer). Answers
// given before the table came in have no row: they are left as they were
// and never counted. The asker marks each one as the source of a rule
// (classify --rule N, or adding a row with the question as a source) or as
// once (for this job only); the plugin's Stop hook keeps a session from
// ending its turn while GET /api/rules/unclassified is not empty.

// RecordAnswer counts a person's answer to a panel question as
// unclassified. It runs in the answering transaction (tx); a question
// answered twice keeps its first row.
func RecordAnswer(tx *sql.Tx, a wire.AnswerRef) error {
	_, err := tx.Exec(`INSERT INTO rule_classes(question_id,asker,header,answered_at) VALUES(?,?,?,?) ON CONFLICT(question_id) DO NOTHING`,
		a.ID, a.Asker, a.Header, a.AnsweredAt.UTC().Format(time.RFC3339Nano))
	return err
}

// classifyBySources marks the counted answers among sources as the source
// of rule id. It runs in the add's transaction.
func classifyBySources(tx *sql.Tx, id int64, sources []string, by, now string) error {
	for _, q := range sources {
		if _, err := tx.Exec(`UPDATE rule_classes SET class=?, rule_id=?, classified_by=?, classified_at=? WHERE question_id=?`,
			wire.AnswerRule, id, by, now, q); err != nil {
			return err
		}
	}
	return nil
}

const classColumns = `question_id,asker,header,answered_at,class,COALESCE(rule_id,0)`

func queryClasses(q queryer, where string, args ...any) ([]wire.AnswerRef, error) {
	rows, err := q.Query(`SELECT `+classColumns+` FROM rule_classes WHERE `+where+` ORDER BY answered_at, rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.AnswerRef{}
	for rows.Next() {
		var a wire.AnswerRef
		var at string
		if err := rows.Scan(&a.ID, &a.Asker, &a.Header, &at, &a.Class, &a.Rule); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
			a.AnsweredAt = t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// unclassifiedAPI is GET /api/rules/unclassified: c's own unclassified
// answers; for a full caller every role's (or ?role='s) with the count per
// role. It carries an ETag and answers 304 to a matching If-None-Match.
func (s *Service) unclassifiedAPI(w http.ResponseWriter, r *http.Request, c Caller) {
	where, args := `class=? AND asker=?`, []any{wire.AnswerUnclassified, c.Role}
	if c.Full {
		where, args = `class=?`, []any{wire.AnswerUnclassified}
		if role := r.URL.Query().Get("role"); role != "" {
			where += ` AND asker=?`
			args = append(args, role)
		}
	}
	answers, err := queryClasses(s.db, where, args...)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	out := wire.Unclassified{Count: len(answers), Answers: answers}
	if c.Full {
		if out.Roles, err = s.unclassifiedByRole(); err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
	}
	body, _ := json.Marshal(out)
	sum := sha256.Sum256(body)
	tag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", tag)
	if etagMatch(r.Header.Get("If-None-Match"), tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(body, '\n'))
}

// classesAPI is GET /api/rules/classes (#581): every counted answer, oldest
// first, with its class and rule, so the dashboard's 결정 view can mark each
// decision 결정 (a rule's source), 한 번 or 미분류. Full callers only.
func (s *Service) classesAPI(w http.ResponseWriter, c Caller) {
	if !c.Full {
		http.Error(w, forbidden, http.StatusForbidden)
		return
	}
	answers, err := queryClasses(s.db, `1=1`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, wire.AnswerClasses{Answers: answers})
}

// unclassifiedByRole is the count of unclassified answers per asker.
func (s *Service) unclassifiedByRole() ([]wire.RoleCount, error) {
	rows, err := s.db.Query(`SELECT asker, COUNT(*) FROM rule_classes WHERE class=? GROUP BY asker ORDER BY asker`, wire.AnswerUnclassified)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.RoleCount{}
	for rows.Next() {
		var rc wire.RoleCount
		if err := rows.Scan(&rc.Role, &rc.Count); err != nil {
			return nil, err
		}
		out = append(out, rc)
	}
	return out, rows.Err()
}

// classifyAPI is POST /api/rules/classify: the asker of a counted answer
// (or a full caller) marks it as the source of rule N, which it must see
// or have proposed, or as once. A classified answer may be marked again.
func (s *Service) classifyAPI(w http.ResponseWriter, r *http.Request, c Caller) {
	var q wire.ClassifyRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&q) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	q.Question = strings.TrimSpace(q.Question)
	if q.Question == "" || (q.Rule > 0) == q.Once || q.Rule < 0 {
		http.Error(w, "question and one of rule (a rule number) or once", http.StatusBadRequest)
		return
	}
	found, err := queryClasses(s.db, `question_id=?`, q.Question)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	if len(found) == 0 || (!c.Full && found[0].Asker != c.Role) {
		http.Error(w, fmt.Sprintf("question %s: no answer of yours to classify", q.Question), http.StatusNotFound)
		return
	}
	class := wire.AnswerOnce
	var rule any
	if q.Rule > 0 {
		class, rule = wire.AnswerRule, q.Rule
		if msg, err := s.classifiableRule(c, q.Rule); err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		} else if msg != "" {
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`UPDATE rule_classes SET class=?, rule_id=?, classified_by=?, classified_at=? WHERE question_id=?`,
		class, rule, c.Role, now, q.Question); err != nil {
		http.Error(w, "could not classify the answer", http.StatusInternalServerError)
		return
	}
	found, err = queryClasses(s.db, `question_id=?`, q.Question)
	if err != nil || len(found) == 0 {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, found[0])
}

// classifiableRule is why c may not name rule id ("" when it may): a full
// caller names any row; another caller a row it sees or one it proposed.
func (s *Service) classifiableRule(c Caller, id int64) (string, error) {
	rw, err := s.get(s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Sprintf("rule: no rule #%d", id), nil
	}
	if err != nil || c.Full || rw.Author == c.Role {
		return "", err
	}
	rd, _, err := s.readerOf(c)
	if err != nil {
		return "", err
	}
	if !rd.sees(rw) {
		return fmt.Sprintf("rule: no rule #%d", id), nil
	}
	return "", nil
}
