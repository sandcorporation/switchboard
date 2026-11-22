package dashboard

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/cli"
)

const pairsFile = "internal/dashboard/cli_pairs.go"

// Every API the dashboard calls (read and write) has a row in CLIPairs: a
// CLI command the CLI takes, or an exception with its reason (#399).
func TestCLIPairs(t *testing.T) {
	calls, bad := dashboardCalls(dashboardHTML)
	for _, b := range bad {
		t.Errorf("cannot read the dashboard call %q: give it a literal method and path, or teach dashboardCalls its wrapper", b)
	}
	rows := map[string]CLIPair{}
	for _, p := range CLIPairs {
		key := p.Method + " " + p.Path
		if _, dup := rows[key]; dup {
			t.Errorf("%s: %s listed twice", pairsFile, key)
		}
		rows[key] = p
		switch {
		case p.CLI == "" && strings.TrimSpace(p.Why) == "":
			t.Errorf("%s: %s has no CLI command and no reason", pairsFile, key)
		case p.CLI != "" && !cli.Known(p.CLI):
			t.Errorf("%s: %s names %q, which is not in the CLI's command table", pairsFile, key, p.CLI)
		}
	}
	for _, c := range calls {
		if _, ok := rows[c]; !ok {
			t.Errorf("the dashboard calls %s with no row in %s: add the CLI command that does the same (or the reason there is none)", c, pairsFile)
		}
	}
	called := map[string]bool{}
	for _, c := range calls {
		called[c] = true
	}
	for key := range rows {
		if !called[key] {
			t.Errorf("%s: %s is no longer called by the dashboard; remove the row", pairsFile, key)
		}
	}
}

// The reader finds calls through api, apiSend, its wrappers and literal
// /api/admin/ URLs, and reports a call it cannot read.
func TestDashboardCallsReader(t *testing.T) {
	src := `api('jobs'+(q.toString()?'?'+q:''));apiSend('POST','roles/'+targetPath(r)+(x?'/revoke':'/restore'));` +
		`termAct(v,'POST','/restart',undefined,'x');autoAct(v,a,'DELETE','','x');apiSend(m,'jobs');` +
		`new WebSocket('wss://'+h+'/api/admin/terminals/'+id+'/socket?t='+k);fetch('/api/admin/me',{})`
	calls, bad := dashboardCalls(src)
	want := []string{"DELETE terminals/{}/autos/{}", "GET jobs", "GET me", "POST roles/{}/restore", "POST roles/{}/revoke", "POST terminals/{}/restart", "WS terminals/{}/socket"}
	if strings.Join(calls, ",") != strings.Join(want, ",") || len(bad) != 1 {
		t.Fatalf("calls %v bad %v", calls, bad)
	}
}

var (
	callRE    = regexp.MustCompile(`\b(api|apiSend|termAct|autoAct)\(`)
	literalRE = regexp.MustCompile(`'/api/admin/`)
	ternaryRE = regexp.MustCompile(`^\(?[^'?]*\?'([^']*)':'([^']*)'\)?$`)
)

// dashboardCalls lists the page's API calls as "METHOD path" (sorted,
// unique), and the calls it could not read.
func dashboardCalls(src string) (calls, bad []string) {
	seen := map[string]bool{}
	add := func(method string, paths []string) {
		for _, p := range paths {
			seen[method+" "+p] = true
		}
	}
	for _, m := range callRE.FindAllStringSubmatchIndex(src, -1) {
		if strings.HasSuffix(src[:m[0]], "function ") {
			continue
		}
		name, args := src[m[2]:m[3]], splitArgs(src[m[1]:])
		switch name {
		case "api":
			add("GET", paths(args[0], ""))
		case "apiSend", "termAct", "autoAct":
			mi, pi, prefix := 0, 1, ""
			if name == "termAct" {
				mi, pi, prefix = 1, 2, "terminals/{}"
			} else if name == "autoAct" {
				mi, pi, prefix = 2, 3, "terminals/{}/autos/{}"
			}
			if len(args) <= pi {
				bad = append(bad, src[m[0]:m[1]])
				continue
			}
			method, ok := literal(args[mi])
			if !ok {
				// The wrappers themselves pass their method on.
				if name == "apiSend" && args[mi] == "method" && strings.HasSuffix(args[pi], "+path") {
					continue
				}
				bad = append(bad, name+"("+strings.Join(args, ",")+")")
				continue
			}
			add(method, paths(args[pi], prefix))
		}
	}
	for _, m := range literalRE.FindAllStringIndex(src, -1) {
		expr := splitArgs(src[m[0]:])[0]
		ps := paths(expr, "")
		if len(ps) == 1 && ps[0] == "{}" {
			continue // api and apiSend
		}
		// The statement so far: a WebSocket or URL constructor makes a socket.
		before := src[max(0, m[0]-160):m[0]]
		if i := strings.LastIndexAny(before, ";\n"); i >= 0 {
			before = before[i+1:]
		}
		method := "GET"
		if strings.Contains(before, "WebSocket(") || strings.Contains(before, "new URL(") {
			method = "WS"
		}
		add(method, ps)
	}
	for c := range seen {
		calls = append(calls, c)
	}
	sort.Strings(calls)
	return calls, bad
}

// paths turns a JS string expression ('a/'+id+'/b'+…) into admin paths: a
// value after a slash is {}, a ternary of two literals gives both, anything
// after the query mark is dropped.
func paths(expr, prefix string) []string {
	out := []string{prefix}
	for _, part := range splitTop(expr, '+') {
		part = strings.TrimSpace(part)
		if lit, ok := literal(part); ok {
			for i := range out {
				out[i] += lit
			}
			continue
		}
		if m := ternaryRE.FindStringSubmatch(part); m != nil {
			var next []string
			for _, o := range out {
				next = append(next, o+m[1], o+m[2])
			}
			out = next
			continue
		}
		for i := range out {
			if strings.HasSuffix(out[i], "/") {
				out[i] += "{}"
			} else if !strings.Contains(out[i], "?") {
				out[i] += "?" // a query built at run time
			}
		}
	}
	for i, o := range out {
		o, _, _ = strings.Cut(o, "?")
		if _, rest, ok := strings.Cut(o, "/api/admin/"); ok {
			o = rest
		}
		out[i] = o
	}
	return out
}

func literal(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' && !strings.Contains(s[1:len(s)-1], "'") {
		return s[1 : len(s)-1], true
	}
	return "", false
}

// splitArgs reads the arguments of a call from just after its open paren up
// to the matching close.
func splitArgs(s string) []string {
	depth, quote := 0, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				return splitTop(s[:i], ',')
			}
			depth--
		case c == ';' && depth == 0:
			return splitTop(s[:i], ',')
		}
	}
	return splitTop(s, ',')
}

// splitTop splits s at sep outside quotes and brackets.
func splitTop(s string, sep byte) []string {
	var out []string
	depth, quote, start := 0, byte(0), 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == sep && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}
