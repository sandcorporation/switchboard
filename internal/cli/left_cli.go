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
// directive left undone (#470): the server refuses its ending answer
// without --left or --left-none, and takes an older CLI's as unrecorded.
func setLeftHeader(req *http.Request) {
	req.Header.Set(config.HeaderPrefix+config.LeftHeaderName, "1")
}

// leftNoteOf is the --left and --left-none of p, nil when neither is given.
// Both together is a usage error.
func leftNoteOf(p parsedArgs) (*wire.LeftNote, error) {
	items := p.multi["--left"]
	none := p.bool("--left-none")
	if len(items) > 0 && none {
		return nil, usagef("use --left or --left-none, not both")
	}
	for _, it := range items {
		if strings.TrimSpace(it) == "" {
			return nil, usagef("--left: an empty item")
		}
	}
	if len(items) == 0 && !none {
		return nil, nil
	}
	return &wire.LeftNote{Left: items, LeftNone: none}, nil
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
	return nil
}

// cliLeft runs left add N TEXT, left issue ID URL and left pass ID
// --directive M.
func cliLeft(c Config, p parsedArgs, out io.Writer) (int, error) {
	if len(p.pos) == 0 {
		return 0, usagef("left: add N TEXT | issue ID URL | pass ID --directive M")
	}
	action, args := p.pos[0], p.pos[1:]
	if action == "issue" || action == "pass" {
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
	default:
		return 0, usagef("left: unknown action %q (add, issue, pass)", action)
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
	fmt.Fprintf(out, "left #%d %s: %s\n", it.ID, it.State, it.Value)
	return ExitOK, nil
}
