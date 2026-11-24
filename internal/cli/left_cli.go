package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// setLeftHeader marks this CLI as one that says what an answer ending a
// directive left undone (#470); the server refuses any ending answer without
// --left or --left-none (#471).
func setLeftHeader(req *http.Request) {
	req.Header.Set(config.HeaderPrefix+config.LeftHeaderName, "1")
}

// verdictsOf reads each --verdict ITEM=done|keep|drop:REASON|issue:URL
// (#565); the server checks the verdicts and the values.
func verdictsOf(p parsedArgs) ([]wire.LeftVerdict, error) {
	var out []wire.LeftVerdict
	for _, raw := range p.multi["--verdict"] {
		item, rest, ok := strings.Cut(strings.TrimSpace(raw), "=")
		id, err := strconv.ParseInt(item, 10, 64)
		if !ok || err != nil || id <= 0 {
			return nil, usagef("--verdict %q: ITEM=done|keep|drop:REASON|issue:URL", raw)
		}
		verdict, value, _ := strings.Cut(rest, ":")
		out = append(out, wire.LeftVerdict{Item: id, Verdict: strings.TrimSpace(verdict), Value: value})
	}
	return out, nil
}

// leftNoteOf is the --left, --left-none and --verdict of p, nil when none is
// given. --left and --left-none together is a usage error.
func leftNoteOf(p parsedArgs) (*wire.LeftNote, error) {
	items := p.multi["--left"]
	none := p.bool("--left-none")
	verdicts, err := verdictsOf(p)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 && none {
		return nil, usagef("use --left or --left-none, not both")
	}
	for _, it := range items {
		if strings.TrimSpace(it) == "" {
			return nil, usagef("--left: an empty item")
		}
	}
	if len(items) == 0 && !none && len(verdicts) == 0 {
		return nil, nil
	}
	return &wire.LeftNote{Left: items, LeftNone: none, Verdicts: verdicts}, nil
}

// noteBody is the request body for note, nil for none.
func noteBody(note *wire.LeftNote) io.Reader {
	if note == nil {
		return nil
	}
	b, _ := json.Marshal(note)
	return bytes.NewReader(b)
}

// withLeft adds --left and --left-none to a send or reply body. Omitted when
// not given, so older servers accept the request unchanged.
func withLeft(p parsedArgs, msg map[string]any) error {
	note, err := leftNoteOf(p)
	if err != nil || note == nil {
		return err
	}
	if len(note.Left) > 0 {
		msg["left"] = note.Left
	}
	if note.LeftNone {
		msg["left_none"] = true
	}
	if len(note.Verdicts) > 0 {
		msg["verdicts"] = note.Verdicts
	}
	return nil
}

// cliLeft runs left add N TEXT, left issue ID URL, left pass ID
// --directive M, left done ID, left drop ID --reason TEXT and left attach ID
// --request-id N (#564).
func cliLeft(c Config, p parsedArgs, out io.Writer) (int, error) {
	if len(p.pos) == 0 {
		return 0, usagef("left: add N TEXT | issue ID URL | pass ID --directive M | done ID | drop ID --reason TEXT | attach ID --request-id N")
	}
	action, args := p.pos[0], p.pos[1:]
	if action == "issue" || action == "pass" || action == "done" || action == "drop" || action == "attach" {
		if len(args) == 0 {
			return 0, usagef("left %s: ID is an item ID", action)
		}
		if id, err := strconv.ParseInt(args[0], 10, 64); err != nil || id <= 0 {
			return 0, usagef("left %s: ID is an item ID", action)
		}
	}
	var path string
	var body any
	switch action {
	case "add":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return 0, usagef("left add N TEXT")
		}
		n, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || n <= 0 {
			return 0, usagef("left add: N is a directive ID")
		}
		path, body = "/api/left", wire.LeftAdd{Directive: n, Left: []string{args[1]}}
	case "issue":
		if len(args) != 2 {
			return 0, usagef("left issue ID URL")
		}
		path, body = "/api/left/"+args[0]+"/issue", wire.LeftEnd{URL: args[1]}
	case "pass":
		if len(args) != 1 {
			return 0, usagef("left pass ID --directive M")
		}
		m, err := strconv.ParseInt(p.values["--directive"], 10, 64)
		if err != nil || m <= 0 {
			return 0, usagef("left pass: --directive M, a directive you sent")
		}
		path, body = "/api/left/"+args[0]+"/pass", wire.LeftEnd{Directive: m}
	case "done":
		if len(args) != 1 {
			return 0, usagef("left done ID")
		}
		path, body = "/api/left/"+args[0]+"/done", wire.LeftEnd{}
	case "drop":
		reason := strings.TrimSpace(p.values["--reason"])
		if len(args) != 1 || reason == "" {
			return 0, usagef("left drop ID --reason TEXT")
		}
		path, body = "/api/left/"+args[0]+"/drop", wire.LeftEnd{Reason: reason}
	case "attach":
		n, err := requestIDOption(p)
		if err != nil {
			return 0, err
		}
		if len(args) != 1 || n == 0 {
			return 0, usagef("left attach ID --request-id N")
		}
		path, body = "/api/left/"+args[0]+"/request", wire.LeftEnd{Request: n}
	default:
		return 0, usagef("left: unknown action %q (add, issue, pass, done, drop, attach)", action)
	}
	b, err := slotCall(c, "POST", path, body)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		fmt.Fprintln(out, strings.TrimSpace(string(b)))
		return ExitOK, nil
	}
	if action == "add" {
		var list wire.LeftList
		if err := json.Unmarshal(b, &list); err != nil {
			return 0, fmt.Errorf("unexpected response from server: %v", err)
		}
		for _, it := range list.Items {
			fmt.Fprintf(out, "left #%d on directive #%d: %s\n", it.ID, it.Directive, it.Text)
		}
		return ExitOK, nil
	}
	var it wire.LeftItem
	if err := json.Unmarshal(b, &it); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if action == "attach" {
		fmt.Fprintf(out, "left #%d on request %d: %s\n", it.ID, it.Request, it.RequestTitle)
		return ExitOK, nil
	}
	if it.Value == "" {
		fmt.Fprintf(out, "left #%d %s\n", it.ID, it.State)
		return ExitOK, nil
	}
	fmt.Fprintf(out, "left #%d %s: %s\n", it.ID, it.State, it.Value)
	return ExitOK, nil
}
