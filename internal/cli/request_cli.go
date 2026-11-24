package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// requestIDOption reads --request-id: 0 when it is not given.
func requestIDOption(p parsedArgs) (int64, error) {
	v := strings.TrimPrefix(strings.TrimSpace(p.values["--request-id"]), "#")
	if v == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return 0, usagef("--request-id: invalid request number %q", p.values["--request-id"])
	}
	return id, nil
}

// withRequestID adds request_id to a mail body when --request-id is given
// (omitted otherwise, so older servers accept the request unchanged).
func withRequestID(p parsedArgs, msg map[string]any) error {
	id, err := requestIDOption(p)
	if err == nil && id > 0 {
		msg["request_id"] = id
	}
	return err
}

// withRequestIDRaw adds request_id to a question file's JSON object.
func withRequestIDRaw(p parsedArgs, body json.RawMessage) (json.RawMessage, error) {
	id, err := requestIDOption(p)
	if err != nil || id == 0 {
		return body, err
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(body, &obj); err != nil {
		return nil, errors.New("question ask --request-id: the file must hold a JSON object or list")
	}
	obj["request_id"] = json.RawMessage(strconv.FormatInt(id, 10))
	return json.Marshal(obj)
}

// requestText is --text, else --text-file, else stdin.
func requestText(p parsedArgs, action string) (string, error) {
	if f := p.values["--text-file"]; f != "" {
		b, err := os.ReadFile(f)
		return string(b), err
	}
	if t := p.values["--text"]; t != "" {
		return t, nil
	}
	st, err := os.Stdin.Stat()
	if err == nil && st.Mode()&os.ModeCharDevice == 0 {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	return "", usagef("request %s: provide --text, --text-file or stdin", action)
}

// cliRequest is `request add|note|wait|done|list|show` over /api/requests (boss
// roles only). Writes print the request number, list one line per request
// and show the request with its linked items; --json prints the server's
// JSON unchanged.
func cliRequest(c Config, p parsedArgs, out io.Writer) (int, error) {
	const actions = "add, note, wait, done, list, show"
	if len(p.pos) == 0 {
		return 0, usagef("request: missing action (%s)", actions)
	}
	action := p.pos[0]
	needID := action == "note" || action == "wait" || action == "done" || action == "show"
	switch {
	case action != "add" && action != "list" && !needID:
		return 0, usagef("request: unknown action %q (%s)", action, actions)
	case needID && len(p.pos) != 2:
		return 0, usagef("request %s: expected a request number", action)
	case !needID && len(p.pos) != 1:
		return 0, usagef("request %s: unexpected argument %q", action, p.pos[1])
	}
	var id int64
	if needID {
		var err error
		if id, err = strconv.ParseInt(strings.TrimPrefix(p.pos[1], "#"), 10, 64); err != nil || id <= 0 {
			return 0, usagef("request %s: invalid request number %q", action, p.pos[1])
		}
	}
	path := "/api/requests/" + strconv.FormatInt(id, 10)
	var b []byte
	var err error
	switch action {
	case "add":
		q := wire.RequestNew{Title: p.values["--title"]}
		if strings.TrimSpace(q.Title) == "" {
			return 0, usagef("request add needs --title")
		}
		if q.Text, err = requestText(p, action); err != nil {
			return 0, err
		}
		b, err = slotCallKey(c, "POST", "/api/requests", q, p.values["--idem"])
	case "note":
		var q wire.RequestNoteNew
		if q.Text, err = requestText(p, action); err != nil {
			return 0, err
		}
		b, err = slotCall(c, "POST", path+"/notes", q)
	case "wait":
		b, err = slotCall(c, "POST", path+"/wait", wire.RequestWaitNew{Off: p.bool("--off")})
	case "done":
		q := wire.RequestDoneNew{Result: p.values["--result"]}
		if strings.TrimSpace(q.Result) == "" {
			return 0, usagef("request done needs --result")
		}
		b, err = slotCall(c, "POST", path+"/done", q)
	case "list":
		return requestList(c, p, out)
	case "show":
		b, err = slotCall(c, "GET", path, nil)
	}
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		_, err = fmt.Fprintln(out, strings.TrimSpace(string(b)))
		return ExitOK, err
	}
	var d wire.RequestDetail
	if err = json.Unmarshal(b, &d); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if action == "show" {
		_, err = io.WriteString(out, formatRequest(d))
		return ExitOK, err
	}
	_, err = fmt.Fprintln(out, d.ID)
	return ExitOK, err
}

func requestList(c Config, p parsedArgs, out io.Writer) (int, error) {
	status := p.values["--status"]
	switch {
	case p.bool("--all") && status != "":
		return 0, usagef("request list: --all and --status do not go together")
	case p.bool("--all"):
	case status == "":
		status = wire.RequestOpen
	}
	path := "/api/requests"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	b, err := slotCall(c, "GET", path, nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		_, err = fmt.Fprintln(out, strings.TrimSpace(string(b)))
		return ExitOK, err
	}
	var l wire.RequestList
	if err = json.Unmarshal(b, &l); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	for _, r := range l.Requests {
		fmt.Fprintln(out, requestLine(r))
	}
	return ExitOK, nil
}

// requestLine: "<id> <status> <created> <title>", then " — <result>" once
// done, or " — <latest news>" before, then " [left N]" while work is left
// under it (#564).
func requestLine(r wire.Request) string {
	line := fmt.Sprintf("%d %s %s %s", r.ID, r.Status, r.CreatedAt.Local().Format("2006-01-02 15:04"), r.Title)
	switch {
	case r.Result != "" && (r.Status == wire.RequestDone || r.Status == wire.RequestChecked):
		line += " — " + r.Result
	case r.Last != nil:
		line += " — " + r.Last.At.Local().Format("15:04") + " " + r.Last.Kind + " " + r.Last.ID + " " + r.Last.Title
	}
	if r.Remaining > 0 {
		line += fmt.Sprintf(" [left %d]", r.Remaining)
	}
	if r.OwnerWait != nil {
		line += " [대표 확인 대기]"
	}
	if r.Overdue {
		line += " [기한 넘김]"
	}
	return line
}

// openItemLine is one line of the work left under a request (#564):
// "mail #N → recipient <time> subject", "question ID <time> header (asker)",
// "left #N <time> text (recorded by)".
func openItemLine(o wire.RequestOpenItem) string {
	at := o.At.Local().Format("2006-01-02 15:04")
	switch o.Kind {
	case wire.RequestOpenMail:
		return fmt.Sprintf("mail #%s → %s %s %s", o.ID, o.Who, at, o.Title)
	case wire.RequestOpenQuestion:
		return fmt.Sprintf("question %s %s %s (%s)", o.ID, at, o.Title, o.Who)
	}
	return fmt.Sprintf("%s #%s %s %s (%s)", o.Kind, o.ID, at, o.Title, o.Who)
}

// formatRequest is `request show`: the request line, the owner's words and
// notes, the work left and the count of what ended (#564), then the linked
// questions, reports and mail.
func formatRequest(d wire.RequestDetail) string {
	var b strings.Builder
	fmt.Fprintln(&b, requestLine(d.Request))
	fmt.Fprintf(&b, "author %s\n", d.Author)
	if d.CheckedAt != nil {
		fmt.Fprintf(&b, "checked %s by %s\n", d.CheckedAt.Local().Format("2006-01-02 15:04"), d.CheckedBy)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, strings.TrimRight(d.Text, "\n"))
	for _, n := range d.NoteList {
		fmt.Fprintf(&b, "\nnote %s\n%s\n", n.CreatedAt.Local().Format("2006-01-02 15:04"), strings.TrimRight(n.Text, "\n"))
	}
	if len(d.RemainingList) > 0 {
		fmt.Fprintln(&b, "\nleft")
		for _, o := range d.RemainingList {
			fmt.Fprintf(&b, "  %s\n", openItemLine(o))
		}
	}
	fmt.Fprintf(&b, "\nended: reports %d · mail %d · left %d\n", d.Finished.Reports, d.Finished.Mail, d.Finished.Left)
	if len(d.QuestionList) > 0 {
		fmt.Fprintln(&b, "\nquestions")
		for _, q := range d.QuestionList {
			fmt.Fprintf(&b, "  %s %s %s | %s", q.ID, q.Status, q.Header, q.Question)
			if len(q.Answers) > 0 {
				fmt.Fprintf(&b, " → %s", strings.Join(q.Answers, " / "))
			}
			fmt.Fprintln(&b)
		}
	}
	if len(d.ReportList) > 0 {
		fmt.Fprintln(&b, "\nreports")
		for _, r := range d.ReportList {
			fmt.Fprintf(&b, "  R%d %s %s\n", r.ID, r.CreatedAt.Local().Format("2006-01-02 15:04"), r.Title)
		}
	}
	if len(d.MailList) > 0 {
		fmt.Fprintln(&b, "\nmail")
		for _, m := range d.MailList {
			fmt.Fprintf(&b, "  #%d %s → %s %s %s\n", m.ID, m.Sender, m.Recipient, m.CreatedAt.Local().Format("2006-01-02 15:04"), m.Subject)
		}
	}
	return b.String()
}
