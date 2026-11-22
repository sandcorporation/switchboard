package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// cliReportRead is `report list [--open]` and `report show ID` (GET
// /api/reports, boss roles only; a group's boss sees its own group's
// reports). --json prints the server's JSON unchanged (for `list` the list
// only; a script reads each report's threads with `show`).
func cliReportRead(c Config, p parsedArgs, out io.Writer) (int, error) {
	switch p.pos[0] {
	case "list":
		if len(p.pos) != 1 {
			return 0, usagef("report list takes no argument")
		}
		return reportList(c, p, out)
	case "show":
		if len(p.pos) != 2 {
			return 0, usagef("report show needs a report ID")
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(p.pos[1], "R"), 10, 64)
		if err != nil || id <= 0 {
			return 0, usagef("report show: invalid report ID %q", p.pos[1])
		}
		return reportShow(c, id, p, out)
	}
	return 0, usagef("report: unknown action %q (list or show)", p.pos[0])
}

func reportList(c Config, p parsedArgs, out io.Writer) (int, error) {
	path := "/api/reports"
	if p.bool("--open") {
		path += "?status=" + url.QueryEscape(wire.ReportOpen)
	}
	b, err := slotCall(c, "GET", path, nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		_, err = fmt.Fprintln(out, strings.TrimSpace(string(b)))
		return ExitOK, err
	}
	var l wire.ReportList
	if err = json.Unmarshal(b, &l); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	for _, rp := range l.Reports {
		fmt.Fprintln(out, reportLine(rp))
		if !p.bool("--open") {
			continue
		}
		d, err := readReport(c, rp.ID)
		if err != nil {
			return 0, err
		}
		for _, t := range d.Threads {
			if t.Open {
				fmt.Fprintln(out, "  "+threadLine(t.Directive))
			}
		}
	}
	return ExitOK, nil
}

func readReport(c Config, id int64) (wire.ReportDetail, error) {
	var d wire.ReportDetail
	b, err := slotCall(c, "GET", "/api/reports/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal(b, &d); err != nil {
		return d, fmt.Errorf("unexpected response from server: %v", err)
	}
	return d, nil
}

func reportShow(c Config, id int64, p parsedArgs, out io.Writer) (int, error) {
	b, err := slotCall(c, "GET", "/api/reports/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		_, err = fmt.Fprintln(out, strings.TrimSpace(string(b)))
		return ExitOK, err
	}
	var d wire.ReportDetail
	if err = json.Unmarshal(b, &d); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	fmt.Fprintln(out, reportLine(d.Report))
	fmt.Fprintf(out, "올린 역할 %s\n", d.Author)
	if d.Request != "" {
		fmt.Fprintf(out, "요청 %s\n", d.Request)
	}
	if d.Body != "" {
		fmt.Fprintf(out, "\n%s\n", strings.TrimRight(d.Body, "\n"))
	}
	for _, t := range d.Threads {
		state := "끝남"
		if t.Open {
			state = "열림"
		}
		fmt.Fprintf(out, "\n%s %s\n", state, threadLine(t.Directive))
		for _, r := range t.Replies {
			mark := ""
			if r.Answers {
				mark = " (답)"
			}
			fmt.Fprintf(out, "  #%d %s → %s %s %s%s %s\n", r.ID, r.Sender, r.Recipient, reportTime(r.CreatedAt), r.Status, mark, r.Subject)
		}
	}
	return ExitOK, nil
}

// reportLine is `<id> <status> 열린 지시 a/b <created> <title>`.
func reportLine(rp wire.Report) string {
	return fmt.Sprintf("%d %s 열린 지시 %d/%d %s %s", rp.ID, rp.Status, rp.OpenDirectives, rp.Directives, reportTime(rp.CreatedAt), rp.Title)
}

// threadLine is `#<id> <sender> → <recipient> <sent> <recipient's status>
// <alive or revoked> <subject>`.
func threadLine(m wire.ReportMail) string {
	live := "살아 있음"
	if !m.RecipientLive {
		live = "폐기됨"
	}
	return fmt.Sprintf("#%d %s → %s %s %s %s %s", m.ID, m.Sender, m.Recipient, reportTime(m.CreatedAt), m.Status, live, m.Subject)
}

func reportTime(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }
