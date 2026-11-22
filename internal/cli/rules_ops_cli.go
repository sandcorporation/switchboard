package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// The ledger's running gear (#409): proposals and their review, the
// proposers, and the classes of a person's answers to panel questions.

// rulesReview is `rules approve N [--replace N[,N] | --alongside]` and
// `rules reject N [--reason TEXT]`. An approval meeting rows in force on
// the same scope and topic prints them and the flags to add, as add does.
func rulesReview(c Config, id int64, action string, p parsedArgs, out io.Writer) (int, error) {
	q := wire.RuleReview{Alongside: p.bool("--alongside"), Reason: p.values["--reason"]}
	if v, ok := p.values["--replace"]; ok {
		var err error
		if q.Replace, err = parseRuleIDs(v); err != nil {
			return 0, err
		}
	}
	if len(q.Replace) > 0 && q.Alongside {
		return 0, usagef("rules %s: --replace and --alongside: choose one", action)
	}
	if action == "reject" && (len(q.Replace) > 0 || q.Alongside) {
		return 0, usagef("rules reject takes --reason only")
	}
	if action == "approve" && q.Reason != "" {
		return 0, usagef("rules approve takes --replace or --alongside, not --reason")
	}
	status, b, err := rulesCall(c, "POST", "/api/rules/"+strconv.FormatInt(id, 10)+"/"+action, q)
	if err != nil {
		return 0, err
	}
	if status == http.StatusConflict && printConflict(out, b, p) {
		return ExitError, nil
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
	for _, old := range r.Replaces {
		fmt.Fprintf(out, "  #%d 대체됨\n", old)
	}
	return ExitOK, nil
}

// rulesProposer is `rules proposer` (the roles that may propose tips) and
// `rules proposer ROLE on|off`.
func rulesProposer(c Config, p parsedArgs, out io.Writer) (int, error) {
	method, body := "GET", any(nil)
	if len(p.pos) == 3 {
		on := p.pos[2]
		if on != "on" && on != "off" {
			return 0, usagef("rules proposer ROLE on|off: got %q", on)
		}
		method, body = "POST", wire.RuleProposer{Role: p.pos[1], On: on == "on"}
	}
	b, err := slotCall(c, method, "/api/rules/proposers", body)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		return printRaw(out, b)
	}
	var l wire.RuleProposers
	if err = json.Unmarshal(b, &l); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if len(l.Proposers) == 0 {
		fmt.Fprintln(out, "제안 역할 없음")
	}
	for _, x := range l.Proposers {
		fmt.Fprintf(out, "%s (켠 이 %s %s)\n", x.Role, x.SetBy, ruleStamp(x.SetAt))
	}
	return ExitOK, nil
}

// rulesClassify is `rules classify QUESTION --rule N | --once`.
func rulesClassify(c Config, p parsedArgs, out io.Writer) (int, error) {
	q := wire.ClassifyRequest{Question: p.pos[1], Once: p.bool("--once")}
	if v, ok := p.values["--rule"]; ok {
		n, err := strconv.ParseInt(strings.TrimPrefix(v, "#"), 10, 64)
		if err != nil || n <= 0 {
			return 0, usagef("--rule: invalid rule number %q", v)
		}
		q.Rule = n
	}
	if (q.Rule > 0) == q.Once {
		return 0, usagef("rules classify needs one of --rule N or --once")
	}
	b, err := slotCall(c, "POST", "/api/rules/classify", q)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		return printRaw(out, b)
	}
	var a wire.AnswerRef
	if err = json.Unmarshal(b, &a); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	fmt.Fprintf(out, "%s %s\n", a.ID, answerClass(a))
	return ExitOK, nil
}

func answerClass(a wire.AnswerRef) string {
	switch a.Class {
	case wire.AnswerRule:
		return fmt.Sprintf("규칙 #%d", a.Rule)
	case wire.AnswerOnce:
		return "이번에만"
	}
	return "미분류"
}

// rulesUnclassified is `rules unclassified [--role ROLE]`: the answers to
// your panel questions still unclassified (every role's, with the count
// per role, for an admin or a global boss).
func rulesUnclassified(c Config, p parsedArgs, out io.Writer) (int, error) {
	path := "/api/rules/unclassified"
	if v := p.values["--role"]; v != "" {
		path += "?" + url.Values{"role": {v}}.Encode()
	}
	b, err := slotCall(c, "GET", path, nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		return printRaw(out, b)
	}
	var u wire.Unclassified
	if err = json.Unmarshal(b, &u); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if u.Count == 0 {
		fmt.Fprintln(out, "미분류 없음")
	}
	for _, a := range u.Answers {
		fmt.Fprintf(out, "%s %s %s %s\n", a.ID, a.Asker, ruleStamp(&a.AnsweredAt), a.Header)
	}
	for _, rc := range u.Roles {
		fmt.Fprintf(out, "  %s %d건\n", rc.Role, rc.Count)
	}
	return ExitOK, nil
}
