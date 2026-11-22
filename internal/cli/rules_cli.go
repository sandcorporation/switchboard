package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// cliRules is `rules ACTION …` over /api/rules (#406 ①): list, show, add,
// void, topics and the tags. An admin's dashboard and a global boss write;
// any other role reads its own scope. --json prints the server's JSON.
func cliRules(c Config, p parsedArgs, out io.Writer) (int, error) {
	action := "list"
	if len(p.pos) > 0 {
		action = p.pos[0]
	}
	nargs := func(n int, what string) error {
		if len(p.pos) != n {
			return usagef("rules %s", what)
		}
		return nil
	}
	switch action {
	case "list":
		if len(p.pos) > 1 {
			return 0, usagef("rules list takes no argument")
		}
		return rulesList(c, p, out)
	case "show", "void":
		if err := nargs(2, action+" needs a rule number"); err != nil {
			return 0, err
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(p.pos[1], "#"), 10, 64)
		if err != nil || id <= 0 {
			return 0, usagef("rules %s: invalid rule number %q", action, p.pos[1])
		}
		if action == "show" {
			return rulesShow(c, id, p, out)
		}
		return rulesVoid(c, id, p, out)
	case "add":
		if err := nargs(2, `add needs the rule text as one argument (quote it)`); err != nil {
			return 0, err
		}
		return rulesAdd(c, p, out)
	case "edit":
		if len(p.pos) != 2 && len(p.pos) != 3 {
			return 0, usagef("rules edit needs a rule number and, to change it, the new text as one argument")
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(p.pos[1], "#"), 10, 64)
		if err != nil || id <= 0 {
			return 0, usagef("rules edit: invalid rule number %q", p.pos[1])
		}
		return rulesEdit(c, id, p, out)
	case "mine":
		if err := nargs(1, "mine takes no argument"); err != nil {
			return 0, err
		}
		return rulesMine(c, p, out)
	case "hit":
		if err := nargs(2, "hit needs rule numbers (N[,N])"); err != nil {
			return 0, err
		}
		return rulesHit(c, p, out)
	case "topics":
		if err := nargs(1, "topics takes no argument"); err != nil {
			return 0, err
		}
		return rulesTopics(c, p, out)
	case "tag":
		return rulesTag(c, p, out)
	}
	return 0, usagef("rules: unknown action %q (list, show, add, edit, void, topics, tag, mine or hit)", action)
}

// rulesCall is slotCall that hands back a non-2xx status and body instead of
// an error, for the add's 409.
func rulesCall(c Config, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, endpoint(c, path), rd)
	req.Header.Set("Content-Type", "application/json")
	authorize(c, req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

func printRaw(out io.Writer, b []byte) (int, error) {
	_, err := fmt.Fprintln(out, strings.TrimSpace(string(b)))
	return ExitOK, err
}

func ruleKind(kind string) string {
	if kind == wire.RuleTip {
		return "요령"
	}
	return "결정"
}

func ruleState(r wire.Rule) string {
	switch r.Status {
	case wire.RuleReplaced:
		if r.ReplacedBy != 0 {
			return fmt.Sprintf(" (대체됨 → #%d)", r.ReplacedBy)
		}
		return " (대체됨)"
	case wire.RuleVoid:
		return " (무효)"
	}
	return ""
}

// ruleLine is one row: number, kind, topic, the scope (full readers) and
// the text's first line.
func ruleLine(r wire.Rule) string {
	scope := ""
	if r.Scope != nil {
		scope = " [" + r.Scope.String()
		if r.Group != "" {
			scope += " @" + r.Group
		}
		scope += "]"
	}
	text, _, _ := strings.Cut(r.Text, "\n")
	return fmt.Sprintf("#%d %s %s%s%s %s", r.ID, ruleKind(r.Kind), r.Topic, scope, ruleState(r), text)
}

func rulesList(c Config, p parsedArgs, out io.Writer) (int, error) {
	q := url.Values{}
	for _, o := range []struct{ flag, param string }{{"--service", "service"}, {"--topic", "topic"}, {"--kind", "kind"}} {
		if v := p.values[o.flag]; v != "" {
			q.Set(o.param, v)
		}
	}
	if p.bool("--all") {
		q.Set("all", "1")
	}
	// Decisions by default (#406, #408): tips with --kind tip, both with
	// --kind all or --all.
	switch kind, ok := p.values["--kind"]; {
	case kind == "all":
		q.Del("kind")
	case !ok && !p.bool("--all"):
		q.Set("kind", wire.RuleDecision)
	}
	path := "/api/rules"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	b, err := slotCall(c, "GET", path, nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		return printRaw(out, b)
	}
	var l wire.RuleList
	if err = json.Unmarshal(b, &l); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if len(l.Rules) == 0 {
		fmt.Fprintln(out, "규칙 없음")
	}
	for _, r := range l.Rules {
		fmt.Fprintln(out, ruleLine(r))
	}
	return ExitOK, nil
}

func ruleStamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

func rulesShow(c Config, id int64, p parsedArgs, out io.Writer) (int, error) {
	b, err := slotCall(c, "GET", "/api/rules/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		return printRaw(out, b)
	}
	var d wire.RuleDetail
	if err = json.Unmarshal(b, &d); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	fmt.Fprintf(out, "#%d %s %s%s\n", d.ID, ruleKind(d.Kind), d.Topic, ruleState(d.Rule))
	fmt.Fprintf(out, "\n%s\n", strings.TrimRight(d.Text, "\n"))
	if d.Scope == nil { // a limited reader gets the row only
		return ExitOK, nil
	}
	fmt.Fprintln(out)
	group := d.Group
	if group == "" {
		group = "(그룹 없음)"
	}
	fmt.Fprintf(out, "범위 %s · 그룹 %s\n", d.Scope.String(), group)
	if len(d.Sources) > 0 {
		fmt.Fprintf(out, "출처 %s\n", strings.Join(d.Sources, ", "))
	}
	if d.SourceText != "" {
		fmt.Fprintf(out, "출처 글 %s\n", d.SourceText)
	}
	if d.Device != "" {
		fmt.Fprintf(out, "장치 %s\n", d.Device)
	}
	if m := ruleMatchText(d.Match); m != "" {
		fmt.Fprintf(out, "걸림 %s\n", m)
	}
	if d.Hits > 0 {
		fmt.Fprintf(out, "걸린 수 %d (마지막 %s)\n", d.Hits, ruleStamp(d.LastHitAt))
	}
	fmt.Fprintf(out, "적은 이 %s %s\n", d.Author, ruleStamp(d.CreatedAt))
	if d.VoidedBy != "" {
		fmt.Fprintf(out, "무효로 돌린 이 %s %s\n", d.VoidedBy, ruleStamp(d.VoidedAt))
	}
	if len(d.History) > 0 {
		fmt.Fprintln(out, "\n이력")
		for _, h := range d.History {
			fmt.Fprintln(out, "  "+ruleLine(h))
		}
	}
	if len(d.Answers) > 0 {
		fmt.Fprintln(out, "\n원문")
		for _, a := range d.Answers {
			fmt.Fprintf(out, "  %s %s → %s %s\n", a.ID, a.Asker, a.AnsweredBy, a.AnsweredAt.Local().Format("2006-01-02 15:04"))
			for _, it := range a.Items {
				chosen := strings.Join(it.Chosen, ", ")
				if it.Other != "" {
					chosen = strings.TrimPrefix(chosen+", "+it.Other, ", ")
				}
				fmt.Fprintf(out, "    [%s] %s → %s\n", it.Header, it.Question, chosen)
			}
		}
	}
	return ExitOK, nil
}

// ruleSources splits --source values: answered question IDs (q_…), or a
// text when a value is not a list of IDs.
func ruleSources(vals []string) (ids []string, text string) {
	var texts []string
	for _, v := range vals {
		parts := strings.Split(v, ",")
		allIDs := true
		for i, x := range parts {
			parts[i] = strings.TrimSpace(x)
			if !strings.HasPrefix(parts[i], "q_") || strings.ContainsAny(parts[i], " \t") {
				allIDs = false
			}
		}
		if allIDs {
			ids = append(ids, parts...)
		} else {
			texts = append(texts, strings.TrimSpace(v))
		}
	}
	return ids, strings.Join(texts, "\n")
}

func parseRuleIDs(v string) ([]int64, error) {
	var out []int64
	for _, x := range strings.Split(v, ",") {
		id, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(x), "#"), 10, 64)
		if err != nil || id <= 0 {
			return nil, usagef("--replace: invalid rule number %q", x)
		}
		out = append(out, id)
	}
	return out, nil
}

func rulesAdd(c Config, p parsedArgs, out io.Writer) (int, error) {
	q := wire.RuleRequest{Kind: p.values["--kind"], Text: p.pos[1], Topic: p.values["--topic"], NewTopic: p.bool("--new-topic"),
		Group: p.values["--group"], Alongside: p.bool("--alongside"), Device: p.values["--device"], IdempotencyKey: p.values["--idem"]}
	scope, ok := p.values["--scope"]
	if !ok || q.Kind == "" || q.Topic == "" {
		return 0, usagef("rules add needs --kind decision|tip, --scope all|service:NAME[,NAME]|tag:NAME[,NAME] and --topic TOPIC")
	}
	var err error
	if q.Scope, err = wire.ParseRuleScope(scope); err != nil {
		return 0, usagef("--scope: %v", err)
	}
	q.Sources, q.SourceText = ruleSources(p.multi["--source"])
	if v, ok := p.values["--replace"]; ok {
		if q.Replace, err = parseRuleIDs(v); err != nil {
			return 0, err
		}
	}
	q.Match = ruleMatchFlags(p, nil)
	return postRule(c, q, "add", p, out)
}

// ruleMatchFlags is base (nil for none) with the --when-* flags given; an
// empty value clears that condition.
func ruleMatchFlags(p parsedArgs, base *wire.RuleMatch) *wire.RuleMatch {
	var m wire.RuleMatch
	if base != nil {
		m = *base
	}
	for _, f := range []struct {
		flag string
		dst  *string
	}{{"--when-tool", &m.Tool}, {"--when-command", &m.Command}, {"--when-path", &m.Path}} {
		if v, ok := p.values[f.flag]; ok {
			*f.dst = v
		}
	}
	if m.Empty() {
		return nil
	}
	return &m
}

// rulesEdit is `rules edit N [TEXT]`: a new row replacing N with its kind,
// topic, scope, group, device, conditions and sources, changed by TEXT,
// --when-*, --device and --source.
func rulesEdit(c Config, id int64, p parsedArgs, out io.Writer) (int, error) {
	b, err := slotCall(c, "GET", "/api/rules/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return 0, err
	}
	var old wire.RuleDetail
	if err = json.Unmarshal(b, &old); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if old.Scope == nil {
		return 0, fmt.Errorf("rule #%d: only an admin or a global boss edits rules", id)
	}
	q := wire.RuleRequest{Kind: old.Kind, Text: old.Text, Topic: old.Topic, Scope: *old.Scope, Group: old.Group, Sources: old.Sources,
		SourceText: old.SourceText, Replace: []int64{id}, Device: old.Device, IdempotencyKey: p.values["--idem"]}
	if len(p.pos) == 3 {
		q.Text = p.pos[2]
	}
	if v, ok := p.values["--device"]; ok {
		q.Device = v
	}
	if vals := p.multi["--source"]; len(vals) > 0 {
		q.Sources, q.SourceText = ruleSources(vals)
	}
	q.Match = ruleMatchFlags(p, old.Match)
	return postRule(c, q, "edit", p, out)
}

// postRule posts q for rules add or edit and prints the row, or on a 409
// the rows in force and the flags to add.
func postRule(c Config, q wire.RuleRequest, action string, p parsedArgs, out io.Writer) (int, error) {
	if err := q.Check(); err != nil {
		return 0, usagef("rules %s: %v", action, err)
	}
	status, b, err := rulesCall(c, "POST", "/api/rules", q)
	if err != nil {
		return 0, err
	}
	if status == http.StatusConflict {
		var cf wire.RuleConflict
		if json.Unmarshal(b, &cf) == nil && len(cf.Rules) > 0 {
			if p.bool("--json") {
				fmt.Fprintln(out, strings.TrimSpace(string(b)))
			} else {
				fmt.Fprintln(out, "같은 범위·주제에 유효한 줄이 있어 올리지 않았다:")
				ids := make([]string, len(cf.Rules))
				for i, r := range cf.Rules {
					fmt.Fprintln(out, "  "+ruleLine(r))
					ids[i] = strconv.FormatInt(r.ID, 10)
				}
				fmt.Fprintf(out, "--replace %s (대체) 또는 --alongside (나란히)를 붙여 다시 올린다.\n", strings.Join(ids, ","))
			}
			return ExitError, nil
		}
	}
	if status >= 300 {
		return 0, fmt.Errorf("server returned %d %s: %s", status, http.StatusText(status), strings.TrimSpace(string(b)))
	}
	if p.bool("--json") {
		return printRaw(out, b)
	}
	var r wire.Rule
	if err = json.Unmarshal(b, &r); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	fmt.Fprintln(out, ruleLine(r))
	if m := ruleMatchText(r.Match); m != "" {
		fmt.Fprintln(out, "  걸림 "+m)
	}
	for _, old := range r.Replaces {
		fmt.Fprintf(out, "  #%d 대체됨\n", old)
	}
	return ExitOK, nil
}

func rulesVoid(c Config, id int64, p parsedArgs, out io.Writer) (int, error) {
	b, err := slotCall(c, "POST", "/api/rules/"+strconv.FormatInt(id, 10)+"/void", nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		return printRaw(out, b)
	}
	var r wire.Rule
	if err = json.Unmarshal(b, &r); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	fmt.Fprintln(out, ruleLine(r))
	return ExitOK, nil
}

func rulesTopics(c Config, p parsedArgs, out io.Writer) (int, error) {
	b, err := slotCall(c, "GET", "/api/rules/topics", nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		return printRaw(out, b)
	}
	var t wire.RuleTopics
	if err = json.Unmarshal(b, &t); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	for _, tp := range t.Topics {
		fmt.Fprintf(out, "%s %d\n", tp.Name, tp.Active)
	}
	return ExitOK, nil
}

func splitTags(v string) []string {
	out := []string{}
	for _, t := range strings.Split(v, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func roleTagsLine(rt wire.RoleTags) string {
	var ch []string
	for _, t := range rt.Add {
		ch = append(ch, "+"+t)
	}
	for _, t := range rt.Remove {
		ch = append(ch, "-"+t)
	}
	tags := strings.Join(rt.Tags, ", ")
	if tags == "" {
		tags = "(태그 없음)"
	}
	return fmt.Sprintf("%s %s → %s", rt.Role, strings.Join(ch, " "), tags)
}

// rulesTag is `rules tag list`, `rules tag set SERVICE TAG[,TAG]` (the
// service's whole set; "" clears it) and `rules tag role ROLE CHANGE[,CHANGE]`
// with +TAG (add), -TAG (remove; after --) or =TAG (back to the service's).
func rulesTag(c Config, p parsedArgs, out io.Writer) (int, error) {
	if len(p.pos) < 2 {
		return 0, usagef("rules tag: list, set SERVICE TAG[,TAG] or role ROLE +TAG|-TAG|=TAG")
	}
	var (
		b   []byte
		err error
	)
	switch p.pos[1] {
	case "list":
		if len(p.pos) != 2 {
			return 0, usagef("rules tag list takes no argument")
		}
		if b, err = slotCall(c, "GET", "/api/rules/tags", nil); err != nil {
			return 0, err
		}
		if p.bool("--json") {
			return printRaw(out, b)
		}
		var t wire.RuleTags
		if err = json.Unmarshal(b, &t); err != nil {
			return 0, fmt.Errorf("unexpected response from server: %v", err)
		}
		for _, s := range t.Services {
			fmt.Fprintf(out, "%s: %s\n", s.Service, strings.Join(s.Tags, ", "))
		}
		for _, r := range t.Roles {
			fmt.Fprintln(out, roleTagsLine(r))
		}
		return ExitOK, nil
	case "set":
		if len(p.pos) != 4 {
			return 0, usagef(`rules tag set SERVICE TAG[,TAG] ("" clears the service's tags)`)
		}
		if b, err = slotCall(c, "PUT", "/api/rules/tags/services", wire.ServiceTags{Service: p.pos[2], Tags: splitTags(p.pos[3])}); err != nil {
			return 0, err
		}
		if p.bool("--json") {
			return printRaw(out, b)
		}
		var s wire.ServiceTags
		if err = json.Unmarshal(b, &s); err != nil {
			return 0, fmt.Errorf("unexpected response from server: %v", err)
		}
		fmt.Fprintf(out, "%s: %s\n", s.Service, strings.Join(s.Tags, ", "))
		return ExitOK, nil
	case "role":
		if len(p.pos) != 4 {
			return 0, usagef("rules tag role ROLE +TAG|-TAG|=TAG[,…] (put -- before a -TAG)")
		}
		ch := wire.RoleTagChange{Role: p.pos[2]}
		for _, x := range splitTags(p.pos[3]) {
			switch x[0] {
			case '+':
				ch.Add = append(ch.Add, x[1:])
			case '-':
				ch.Remove = append(ch.Remove, x[1:])
			case '=':
				ch.Inherit = append(ch.Inherit, x[1:])
			default:
				return 0, usagef("rules tag role: %q: +TAG adds, -TAG removes, =TAG goes back to the service's", x)
			}
		}
		if b, err = slotCall(c, "POST", "/api/rules/tags/roles", ch); err != nil {
			return 0, err
		}
		if p.bool("--json") {
			return printRaw(out, b)
		}
		var rt wire.RoleTags
		if err = json.Unmarshal(b, &rt); err != nil {
			return 0, fmt.Errorf("unexpected response from server: %v", err)
		}
		fmt.Fprintln(out, roleTagsLine(rt))
		return ExitOK, nil
	}
	return 0, usagef("rules tag: unknown action %q (list, set or role)", p.pos[1])
}

// ruleMatchText is a row's conditions: 도구 T · 명령 /RE/ · 경로 GLOB.
func ruleMatchText(m *wire.RuleMatch) string {
	if m == nil {
		return ""
	}
	var parts []string
	if m.Tool != "" {
		parts = append(parts, "도구 "+m.Tool)
	}
	if m.Command != "" {
		parts = append(parts, "명령 /"+m.Command+"/")
	}
	if m.Path != "" {
		parts = append(parts, "경로 "+m.Path)
	}
	return strings.Join(parts, " · ")
}

// rulesMine is `rules mine`: the rows a session hook of this role matches
// tool calls against (GET /api/rules/mine), one per line with its
// conditions; a row without one goes into directive mail instead.
func rulesMine(c Config, p parsedArgs, out io.Writer) (int, error) {
	b, err := slotCall(c, "GET", "/api/rules/mine", nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		return printRaw(out, b)
	}
	var h wire.RuleHints
	if err = json.Unmarshal(b, &h); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if len(h.Rules) == 0 {
		fmt.Fprintln(out, "규칙 없음")
	}
	for _, r := range h.Rules {
		line := fmt.Sprintf("#%d %s %s %s", r.ID, ruleKind(r.Kind), r.Topic, r.Summary)
		if m := ruleMatchText(r.Match); m != "" {
			line += " [" + m + "]"
		}
		fmt.Fprintln(out, line)
	}
	return ExitOK, nil
}

// rulesHit is `rules hit N[,N]`: counts a hit of these rows (POST
// /api/rules/hits); rows outside the role's are left out.
func rulesHit(c Config, p parsedArgs, out io.Writer) (int, error) {
	ids, err := parseRuleIDs(p.pos[1])
	if err != nil {
		return 0, err
	}
	b, err := slotCall(c, "POST", "/api/rules/hits", wire.RuleHitsRequest{IDs: ids})
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		return printRaw(out, b)
	}
	var res wire.RuleHitsResult
	if err = json.Unmarshal(b, &res); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	parts := make([]string, len(res.Counted))
	for i, id := range res.Counted {
		parts[i] = "#" + strconv.FormatInt(id, 10)
	}
	if len(parts) == 0 {
		fmt.Fprintln(out, "센 줄 없음")
	} else {
		fmt.Fprintln(out, "센 줄 "+strings.Join(parts, " "))
	}
	return ExitOK, nil
}
