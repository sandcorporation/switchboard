package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/store"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// API serves /api/rules (rest after "rules/", "" for the list) for c:
//
//	GET    ""            the rows c sees (?service=&topic=&kind=&all=1)
//	POST   ""            add a row (full callers; 409 on a conflict)
//	GET    {id}          one row; for a full caller with its history and answers
//	POST   {id}/void     void a row in force (full callers)
//	POST   {id}/approve  put a proposed row in force (full callers; 409 on a conflict)
//	POST   {id}/reject   turn a proposed row down (full callers)
//	GET    proposers     the roles that may propose tips (full callers)
//	POST   proposers     set a role's proposing on or off (full callers)
//	POST   classify      classify a person's answer to c's question
//	GET    unclassified  the answers still unclassified (ETag)
//	GET    classes       every counted answer with its class (full callers)
//	GET    topics        the topics, with the rows in force c sees
//	GET    mine          the rows in force reaching c's role, for its session hook (ETag)
//	POST   hits          count hits of c's rows ({"ids":[…]})
//	GET    tags          the service and role tags (full callers)
//	PUT    tags/services set a service's tags (full callers)
//	POST   tags/roles    change a role's tags (full callers)
func (s *Service) API(w http.ResponseWriter, r *http.Request, c Caller, rest string) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case rest == "" && r.Method == http.MethodGet:
		s.listAPI(w, r, c)
	case rest == "" && r.Method == http.MethodPost:
		s.addAPI(w, r, c)
	case rest == "topics" && r.Method == http.MethodGet:
		s.topicsAPI(w, c)
	case rest == "mine" && r.Method == http.MethodGet:
		s.mineAPI(w, r, c)
	case rest == "hits" && r.Method == http.MethodPost:
		s.hitsAPI(w, r, c)
	case rest == "proposers":
		s.proposersAPI(w, r, c)
	case rest == "classify" && r.Method == http.MethodPost:
		s.classifyAPI(w, r, c)
	case rest == "unclassified" && r.Method == http.MethodGet:
		s.unclassifiedAPI(w, r, c)
	case rest == "classes" && r.Method == http.MethodGet:
		s.classesAPI(w, c)
	case rest == "tags" || strings.HasPrefix(rest, "tags/"):
		s.tagsAPI(w, r, c, strings.TrimPrefix(strings.TrimPrefix(rest, "tags"), "/"))
	default:
		idText, action, _ := strings.Cut(rest, "/")
		id, err := strconv.ParseInt(strings.TrimPrefix(idText, "#"), 10, 64)
		if err != nil || id <= 0 {
			http.NotFound(w, r)
			return
		}
		switch {
		case action == "" && r.Method == http.MethodGet:
			s.showAPI(w, r, c, id)
		case action == "void" && r.Method == http.MethodPost:
			s.voidAPI(w, r, c, id)
		case action == "approve" && r.Method == http.MethodPost:
			s.approveAPI(w, r, c, id)
		case action == "reject" && r.Method == http.MethodPost:
			s.rejectAPI(w, r, c, id)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

const forbidden = "rules are written by an admin or a global boss (SWITCHBOARD_BOSS_ROLES)"

// row is a stored rule with the fields the read rules need.
type row struct {
	wire.Rule
	scope wire.RuleScope
}

const ruleColumns = `id,kind,body,grp,scope,scope_values,topic,sources,source_text,status,replaces,replaced_by,device,match_tool,match_command,match_path,author,created_at,voided_by,voided_at,reviewed_by,reviewed_at,review_note`

func splitList(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

func scanRule(sc interface{ Scan(...any) error }) (row, error) {
	var (
		rw                               row
		scopeVals, sources, replaces     string
		created, voidedAt, reviewedAt    string
		replacedBy                       sql.NullInt64
		matchTool, matchCommand, matchPa string
	)
	err := sc.Scan(&rw.ID, &rw.Kind, &rw.Text, &rw.Group, &rw.scope.Kind, &scopeVals, &rw.Topic, &sources, &rw.SourceText,
		&rw.Status, &replaces, &replacedBy, &rw.Device, &matchTool, &matchCommand, &matchPa, &rw.Author, &created, &rw.VoidedBy, &voidedAt,
		&rw.ReviewedBy, &reviewedAt, &rw.ReviewNote)
	if err != nil {
		return rw, err
	}
	rw.scope.Values = splitList(scopeVals)
	sc2 := rw.scope
	rw.Scope = &sc2
	rw.Sources = splitList(sources)
	for _, x := range splitList(replaces) {
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			rw.Replaces = append(rw.Replaces, n)
		}
	}
	rw.ReplacedBy = replacedBy.Int64
	if m := (wire.RuleMatch{Tool: matchTool, Command: matchCommand, Path: matchPa}); !m.Empty() {
		rw.Match = &m
	}
	if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
		rw.CreatedAt = &t
	}
	if t, err := time.Parse(time.RFC3339Nano, voidedAt); err == nil {
		rw.VoidedAt = &t
	}
	if t, err := time.Parse(time.RFC3339Nano, reviewedAt); err == nil {
		rw.ReviewedAt = &t
	}
	return rw, nil
}

type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func queryRules(q queryer, where string, args ...any) ([]row, error) {
	rows, err := q.Query(`SELECT `+ruleColumns+` FROM rules WHERE `+where+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		rw, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rw)
	}
	return out, rows.Err()
}

// reader is a limited caller's scope: its group, service and tags.
type reader struct {
	group, service string
	tags           []string
}

// readerOf is c's scope; ok is false for a full caller, who reads all.
func (s *Service) readerOf(c Caller) (reader, bool, error) {
	if c.Full {
		return reader{}, false, nil
	}
	tags, err := s.tagsOf(s.db, c.Role)
	return reader{group: c.Group, service: ServiceOf(c.Role), tags: tags}, true, err
}

// applies reports whether rw reaches a role of service svc carrying tags.
// The group is checked by the caller.
func applies(rw row, svc string, tags []string) bool {
	switch rw.scope.Kind {
	case wire.ScopeAll:
		return true
	case wire.ScopeService:
		return slices.Contains(rw.scope.Values, svc)
	case wire.ScopeTag:
		for _, t := range tags {
			if slices.Contains(rw.scope.Values, t) {
				return true
			}
		}
	}
	return false
}

// sees reports whether a limited reader sees rw: a row in force of its own
// group that applies to it.
func (rd reader) sees(rw row) bool {
	return rw.Status == wire.RuleActive && rw.Group == rd.group && applies(rw, rd.service, rd.tags)
}

// brief is what a limited reader gets of a row: number, kind, text, topic.
func brief(rw row) wire.Rule {
	return wire.Rule{ID: rw.ID, Kind: rw.Kind, Text: rw.Text, Topic: rw.Topic}
}

// listAPI is GET /api/rules. service keeps the rows that apply to that
// service's roles (its group's all-scope rows, its own rows and its tags'
// rows); topic and kind filter; all=1 adds replaced and void rows (full
// callers only).
func (s *Service) listAPI(w http.ResponseWriter, r *http.Request, c Caller) {
	q := r.URL.Query()
	kind, topic, svc := q.Get("kind"), q.Get("topic"), q.Get("service")
	all := q.Get("all") == "1" || q.Get("all") == "true"
	proposed := q.Get("proposed") == "1" || q.Get("proposed") == "true"
	if kind != "" && kind != wire.RuleDecision && kind != wire.RuleTip {
		http.Error(w, "kind: decision or tip", http.StatusBadRequest)
		return
	}
	rd, limited, err := s.readerOf(c)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	if (all || proposed) && limited {
		http.Error(w, "all (replaced, void and proposed rows) and proposed are for an admin or a global boss", http.StatusForbidden)
		return
	}
	where, args := `1=1`, []any{}
	switch {
	case proposed:
		where += ` AND status=?`
		args = append(args, wire.RuleProposed)
	case !all:
		where += ` AND status=?`
		args = append(args, wire.RuleActive)
	}
	if kind != "" {
		where += ` AND kind=?`
		args = append(args, kind)
	}
	if topic != "" {
		where += ` AND topic=?`
		args = append(args, topic)
	}
	rows, err := queryRules(s.db, where, args...)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	var svcTags []string
	if svc != "" {
		if svcTags, err = s.serviceTags(s.db, svc); err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
	}
	var hc map[int64]hitCount
	out := wire.RuleList{Rules: []wire.Rule{}, Full: !limited}
	if !limited {
		if hc, err = s.hitCounts(); err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		if err = s.db.QueryRow(`SELECT COUNT(*) FROM rules WHERE status=?`, wire.RuleProposed).Scan(&out.Proposed); err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
	}
	for _, rw := range rows {
		if limited && !rd.sees(rw) {
			continue
		}
		if svc != "" && (rw.Group != s.groupOfService(svc) || !applies(rw, svc, svcTags)) {
			continue
		}
		if limited {
			out.Rules = append(out.Rules, brief(rw))
		} else {
			out.Rules = append(out.Rules, withHits(rw.Rule, hc))
		}
	}
	writeJSON(w, out)
}

func (s *Service) get(q queryer, id int64) (row, error) {
	rows, err := queryRules(q, `id=?`, id)
	if err != nil {
		return row{}, err
	}
	if len(rows) == 0 {
		return row{}, sql.ErrNoRows
	}
	return rows[0], nil
}

// history is every row linked to id through replacement, oldest first,
// without id itself.
func (s *Service) history(rw row) ([]wire.Rule, error) {
	seen := map[int64]bool{rw.ID: true}
	queue := []row{rw}
	var out []wire.Rule
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		next := slices.Clone(cur.Replaces)
		if cur.ReplacedBy != 0 {
			next = append(next, cur.ReplacedBy)
		}
		for _, id := range next {
			if seen[id] {
				continue
			}
			seen[id] = true
			o, err := s.get(s.db, id)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			out = append(out, o.Rule)
			queue = append(queue, o)
		}
	}
	slices.SortFunc(out, func(a, b wire.Rule) int { return int(a.ID - b.ID) })
	return out, nil
}

// showAPI is GET /api/rules/{id}. A limited reader gets a row it sees, as
// in the list; a full caller the row, its history and its answers.
func (s *Service) showAPI(w http.ResponseWriter, r *http.Request, c Caller, id int64) {
	rw, err := s.get(s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	rd, limited, err := s.readerOf(c)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	if limited {
		if !rd.sees(rw) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, wire.RuleDetail{Rule: brief(rw)})
		return
	}
	hc, err := s.hitCounts()
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	d := wire.RuleDetail{Rule: withHits(rw.Rule, hc)}
	if d.History, err = s.history(rw); err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	if len(rw.Sources) > 0 && s.hooks.Answers != nil {
		if d.Answers, err = s.hooks.Answers(rw.Sources); err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, d)
}

// overlaps reports whether two scopes reach a common role: both all, or
// the same kind sharing a service or a tag.
func overlaps(a, b wire.RuleScope) bool {
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind == wire.ScopeAll {
		return true
	}
	for _, v := range a.Values {
		if slices.Contains(b.Values, v) {
			return true
		}
	}
	return false
}

// conflictError is the 409 of an add: rows in force on an overlapping
// scope of the same group and topic, not named in replace.
type conflictError struct{ rows []wire.Rule }

func (conflictError) Error() string { return "conflict" }

// badRequest is a 400 found inside the add's transaction.
type badRequest string

func (e badRequest) Error() string { return string(e) }

// ruleGroup is the group a new row belongs to: the services' group for a
// service scope (every service in one group), else the asked group ("" for
// roles in no group). A named group must be one.
func (s *Service) ruleGroup(q wire.RuleRequest) (string, error) {
	if q.Group != "" && s.groupOfService(q.Group) != q.Group {
		return "", fmt.Errorf("group %q: no such group", q.Group)
	}
	if q.Scope.Kind != wire.ScopeService {
		return q.Group, nil
	}
	g := s.groupOfService(q.Scope.Values[0])
	for _, svc := range q.Scope.Values[1:] {
		if s.groupOfService(svc) != g {
			return "", errors.New("scope: the services are in different groups; add one row per group")
		}
	}
	if q.Group != "" && q.Group != g {
		return "", fmt.Errorf("group %q: the scope's services are in group %q", q.Group, g)
	}
	return g, nil
}

// checkSources keeps only answered questions as sources.
func (s *Service) checkSources(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if s.hooks.Answers == nil {
		return errors.New("sources: answered questions cannot be read here")
	}
	ds, err := s.hooks.Answers(ids)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for _, d := range ds {
		found[d.ID] = true
	}
	var bad []string
	for _, id := range ids {
		if !found[id] {
			bad = append(bad, id)
		}
	}
	if len(bad) > 0 {
		return badRequest("sources: no answered question " + strings.Join(bad, ", "))
	}
	return nil
}

func joinInts(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// addAPI is POST /api/rules: a full caller adds a row. When rows in force
// of the same group and topic overlap its scope, it goes in only with
// replace (naming every one of them, or some with alongside for the rest)
// or alongside; otherwise 409 with those rows. Replaced rows keep their
// history and point at the new row. A retry with the same idempotency key
// returns the first row with 200.
func (s *Service) addAPI(w http.ResponseWriter, r *http.Request, c Caller) {
	proposer := false
	if !c.Full {
		ok, err := s.isProposer(c.Role)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, forbidden+"; a role an admin lets propose (rules proposer ROLE on) adds tips", http.StatusForbidden)
			return
		}
		proposer = true
	}
	var q wire.RuleRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&q) != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := q.Check(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status := wire.RuleActive
	if proposer {
		var msg string
		if status, msg = proposal(c, &q); msg != "" {
			code := http.StatusBadRequest
			if q.Kind != wire.RuleTip {
				code = http.StatusForbidden
			}
			http.Error(w, msg, code)
			return
		}
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		key = q.IdempotencyKey
	}
	if key != "" {
		if rw, ok := s.byIdem(c.Role, key); ok {
			writeJSON(w, rw.Rule)
			return
		}
	}
	group, err := s.ruleGroup(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.checkSources(q.Sources); err != nil {
		var bad badRequest
		if errors.As(err, &bad) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, "query failed", http.StatusInternalServerError)
		}
		return
	}
	id, err := s.add(r.Context(), c, q, group, key, status)
	var conflict conflictError
	var bad badRequest
	switch {
	case errors.As(err, &conflict):
		writeConflict(w, conflict, c.Full)
		return
	case errors.As(err, &bad):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case err != nil:
		// A concurrent retry with the same key won the insert.
		if rw, ok := s.byIdem(c.Role, key); key != "" && ok {
			writeJSON(w, rw.Rule)
			return
		}
		http.Error(w, "could not store the rule", http.StatusInternalServerError)
		return
	}
	rw, err := s.get(s.db, id)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	if status == wire.RuleProposed {
		s.noticeProposal(rw)
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, rw.Rule)
}

// writeConflict is the 409 of an add or an approval. A proposer gets the
// rows as a limited reader does.
func writeConflict(w http.ResponseWriter, conflict conflictError, full bool) {
	rows := conflict.rows
	if !full {
		rows = make([]wire.Rule, len(conflict.rows))
		for i, o := range conflict.rows {
			rows[i] = wire.Rule{ID: o.ID, Kind: o.Kind, Text: o.Text, Topic: o.Topic}
		}
	}
	w.WriteHeader(http.StatusConflict)
	writeJSON(w, wire.RuleConflict{
		Error: "rules in force on this scope and topic; post again with replace (the rows to replace) or alongside",
		Rules: rows,
	})
}

func (s *Service) byIdem(author, key string) (row, bool) {
	rows, err := queryRules(s.db, `author=? AND idem=?`, author, key)
	if err != nil || len(rows) == 0 {
		return row{}, false
	}
	return rows[0], true
}

func (s *Service) add(ctx context.Context, c Caller, q wire.RuleRequest, group, key, status string) (int64, error) {
	var id int64
	now := s.now().UTC().Format(time.RFC3339Nano)
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		var known int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM rule_topics WHERE name=?`, q.Topic).Scan(&known); err != nil {
			return err
		}
		if known == 0 {
			if !q.NewTopic {
				return badRequest(fmt.Sprintf("topic %q: not a topic yet; pick one (rules topics) or create it with new_topic (--new-topic)", q.Topic))
			}
			if _, err := tx.Exec(`INSERT INTO rule_topics(name,created_by,created_at) VALUES(?,?,?)`, q.Topic, c.Role, now); err != nil {
				return err
			}
		}
		inForce, err := queryRules(tx, `status=? AND grp=? AND topic=?`, wire.RuleActive, group, q.Topic)
		if err != nil {
			return err
		}
		if err := s.checkReplace(tx, q.Replace, group, c); err != nil {
			return err
		}
		if status == wire.RuleActive && !q.Alongside {
			if left := leftInForce(inForce, q.Scope, q.Replace, 0); len(left) > 0 {
				return conflictError{left}
			}
		}
		var m wire.RuleMatch
		if q.Match != nil {
			m = *q.Match
		}
		res, err := tx.Exec(`INSERT INTO rules(kind,body,grp,scope,scope_values,topic,sources,source_text,status,replaces,device,match_tool,match_command,match_path,author,created_at,idem)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			q.Kind, q.Text, group, q.Scope.Kind, strings.Join(q.Scope.Values, ","), q.Topic, strings.Join(q.Sources, ","), q.SourceText,
			status, joinInts(q.Replace), strings.TrimSpace(q.Device), m.Tool, m.Command, m.Path, c.Role, now, nullable(key))
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		if err := replaceRows(tx, id, q.Replace); err != nil {
			return err
		}
		return classifyBySources(tx, id, q.Sources, c.Role, now)
	})
	return id, err
}

// leftInForce is the rows of inForce (other than self) whose scope
// overlaps scope and that replace does not name: the rows a new row in
// force would stand beside without the writer choosing so.
func leftInForce(inForce []row, scope wire.RuleScope, replace []int64, self int64) []wire.Rule {
	var left []wire.Rule
	for _, o := range inForce {
		if o.ID != self && overlaps(o.scope, scope) && !slices.Contains(replace, o.ID) {
			left = append(left, o.Rule)
		}
	}
	return left
}

// replaceRows marks olds replaced by id.
func replaceRows(tx *sql.Tx, id int64, olds []int64) error {
	for _, old := range olds {
		if _, err := tx.Exec(`UPDATE rules SET status=?, replaced_by=? WHERE id=?`, wire.RuleReplaced, id, old); err != nil {
			return err
		}
	}
	return nil
}

// voidAPI is POST /api/rules/{id}/void: a full caller takes a row in force
// out of force. It stays in the history.
func (s *Service) voidAPI(w http.ResponseWriter, r *http.Request, c Caller, id int64) {
	if !c.Full {
		http.Error(w, forbidden, http.StatusForbidden)
		return
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`UPDATE rules SET status=?, voided_by=?, voided_at=? WHERE id=? AND status=?`, wire.RuleVoid, c.Role, now, id, wire.RuleActive)
	if err != nil {
		http.Error(w, "could not void the rule", http.StatusInternalServerError)
		return
	}
	rw, err := s.get(s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, fmt.Sprintf("rule #%d is %s, not in force", id, rw.Status), http.StatusConflict)
		return
	}
	writeJSON(w, rw.Rule)
}

// topicsAPI is GET /api/rules/topics: every topic with the rows in force
// c sees under it. A limited reader gets only topics it has rows under.
func (s *Service) topicsAPI(w http.ResponseWriter, c Caller) {
	rd, limited, err := s.readerOf(c)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	names, err := s.db.Query(`SELECT name FROM rule_topics ORDER BY name`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	counts := map[string]int{}
	var order []string
	for names.Next() {
		var n string
		if err := names.Scan(&n); err != nil {
			names.Close()
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		order = append(order, n)
		counts[n] = 0
	}
	names.Close()
	rows, err := queryRules(s.db, `status=?`, wire.RuleActive)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	for _, rw := range rows {
		if !limited || rd.sees(rw) {
			counts[rw.Topic]++
		}
	}
	out := wire.RuleTopics{Topics: []wire.RuleTopic{}}
	for _, n := range order {
		if limited && counts[n] == 0 {
			continue
		}
		out.Topics = append(out.Topics, wire.RuleTopic{Name: n, Active: counts[n]})
	}
	writeJSON(w, out)
}
