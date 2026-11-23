package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// cliHooks prints GET /api/hooks (#527): each webhook's name, receiving
// role, mode and last notice. Neither its address nor its destination is
// ever in the response.
func cliHooks(c Config, p parsedArgs, out io.Writer) (int, error) {
	if len(p.pos) != 1 || p.pos[0] != "list" {
		return 0, usagef("usage: hooks list [--json]")
	}
	b, err := slotCall(c, "GET", "/api/hooks", nil)
	if err != nil {
		return 0, err
	}
	if p.bool("--json") {
		fmt.Fprintln(out, strings.TrimSpace(string(b)))
		return ExitOK, nil
	}
	var list []wire.HookSummary
	if err = json.Unmarshal(b, &list); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if len(list) == 0 {
		fmt.Fprintln(out, "no hooks")
		return ExitOK, nil
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tROLE\tMODE\tLAST")
	for _, h := range list {
		last := "-"
		if t, err := time.Parse(time.RFC3339Nano, h.LastAt); err == nil {
			last = t.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", h.Name, h.Role, h.Mode, last)
	}
	return ExitOK, tw.Flush()
}
