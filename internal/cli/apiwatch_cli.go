package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// cliWatchList is `switchboard watch list [--json]` (#528): GET
// /api/watches, the server's API watches (not the inbox watch or a `watch
// start` command, which `job list` shows).
func cliWatchList(c Config, p parsedArgs) (int, error) {
	b, err := slotCall(c, "GET", "/api/watches", nil)
	if err != nil {
		return 0, err
	}
	var rows []wire.WatchSummary
	if err = json.Unmarshal(b, &rows); err != nil {
		return 0, fmt.Errorf("unexpected response from server: %v", err)
	}
	if p.bool("--json") {
		fmt.Println(strings.TrimSpace(string(b)))
		return ExitOK, nil
	}
	if len(rows) == 0 {
		fmt.Println("no watches")
		return ExitOK, nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTO\tEVERY\tLAST\tRESULT")
	for _, w := range rows {
		last, result := "-", w.LastResult
		if t, err := time.Parse(time.RFC3339, w.LastAt); err == nil {
			last = t.Local().Format("01-02 15:04")
		}
		if !w.Enabled {
			result = "off"
		} else if result == "" {
			result = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%dm\t%s\t%s\n", w.Name, w.To, w.Every, last, result)
	}
	return ExitOK, tw.Flush()
}
