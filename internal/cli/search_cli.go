package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// searchQueryOptions maps CLI options with a value to GET /api/search
// parameters.
var searchQueryOptions = []struct{ flag, param string }{
	{"--role", "role"}, {"--thread", "thread"}, {"--since", "since"}, {"--until", "until"}, {"--limit", "limit"}, {"--after", "after"},
}

// cliSearch reads GET /api/search (#481): two lines a result, then where
// the next page starts or that the time limit cut the search; the JSON
// unchanged with --json.
func cliSearch(c Config, p parsedArgs, out io.Writer) (int, error) {
	q := url.Values{"q": {p.pos[0]}}
	if p.bool("--regex") {
		q.Set("regex", "1")
	}
	if p.bool("--case") {
		q.Set("case", "1")
	}
	for _, k := range p.multi["--kind"] {
		q.Add("kind", k)
	}
	for _, o := range searchQueryOptions {
		if v, ok := p.values[o.flag]; ok && v != "" {
			q.Set(o.param, v)
		}
	}
	b, err := slotCall(c, "GET", "/api/search?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		_, err = fmt.Fprintln(out, strings.TrimSpace(string(b)))
		return ExitOK, err
	}
	var res wire.SearchResult
	if err = json.Unmarshal(b, &res); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	_, err = io.WriteString(out, formatSearch(res))
	return ExitOK, err
}

// formatSearch renders a page: per hit a head line (kind, id, local time,
// who, the command that opens it) and the matched line indented.
func formatSearch(res wire.SearchResult) string {
	var b strings.Builder
	for _, h := range res.Hits {
		who := h.From
		if h.Kind == wire.SearchMail && h.To != "" {
			who += " → " + h.To
		}
		id := "#" + h.ID
		switch h.Kind {
		case wire.SearchReport:
			id = "R" + h.ID
		case wire.SearchQuestion:
			id = h.ID
		}
		fmt.Fprintf(&b, "%s %s %s %s · %s\n", h.Kind, id, h.At.Local().Format("2006-01-02 15:04"), who, h.Open)
		fmt.Fprintf(&b, "  %s\n", oneLine(h.Line))
	}
	if len(res.Hits) == 0 {
		b.WriteString("없음\n")
	}
	if res.Cut {
		b.WriteString("끊김: 시간 상한에 멈췄다. 기간·종류·역할로 좁혀 다시 찾는다\n")
	}
	if res.Next != "" {
		fmt.Fprintf(&b, "더 있음: --after %s\n", res.Next)
	}
	return b.String()
}
