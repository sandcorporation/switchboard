package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sandcorporation/switchboard/internal/ownerfile"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// cliHooks runs `hooks` (#527, #549): list prints GET /api/hooks, each
// webhook's name, receiving role, mode and last notice; add, edit, rm and
// rotate are a global boss's writes at /api/boss/hooks…. Neither an
// address nor an original destination is ever printed: the address goes to
// --address-file only, the destination comes from a file or stdin.
func cliHooks(c Config, p parsedArgs, stdin io.Reader, out io.Writer) (int, error) {
	action := "list"
	if len(p.pos) > 0 {
		action = p.pos[0]
	}
	switch action {
	case "list":
		if len(p.pos) > 1 {
			return 0, usagef("usage: hooks list [--json]")
		}
		return hooksList(c, p, out)
	case "add":
		if len(p.pos) != 1 {
			return 0, usagef("usage: hooks add --name NAME --to ROLE --address-file PATH [options]")
		}
		return hooksAdd(c, p, stdin, out)
	case "edit", "rm", "rotate":
		if len(p.pos) != 2 || p.pos[1] == "" {
			return 0, usagef("hooks %s requires NAME", action)
		}
	default:
		return 0, usagef("hooks: unknown action %q (list, add, edit, rm, rotate)", action)
	}
	name, jsonOut := p.pos[1], p.bool("--json")
	switch action {
	case "rm":
		if !confirmed(p, out, "remove the webhook "+name+": its address stops working for good") {
			return ExitUsage, nil
		}
		h, err := hookByName(c, name)
		if err != nil {
			return 0, err
		}
		return bossSend(c, "DELETE", "hooks/"+strconv.FormatInt(h.ID, 10), nil, jsonOut, out, "removed the webhook "+name)
	case "rotate":
		return hooksRotate(c, p, name, out)
	}
	return hooksEdit(c, p, name, stdin, out)
}

func hooksList(c Config, p parsedArgs, out io.Writer) (int, error) {
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

// hooksAdd adds a webhook: the address file is made (new, owner-only)
// before the server is asked, so an existing file stops it with nothing
// made.
func hooksAdd(c Config, p parsedArgs, stdin io.Reader, out io.Writer) (int, error) {
	v := wire.HookSave{Name: strings.TrimSpace(p.values["--name"]), Role: strings.TrimSpace(p.values["--to"]), Mode: wire.HookModeMail, Urgent: p.bool("--urgent")}
	path := p.values["--address-file"]
	if v.Name == "" || v.Role == "" || path == "" {
		return 0, usagef("hooks add requires --name, --to and --address-file")
	}
	if p.bool("--no-urgent") || p.bool("--clear-destination") || p.values["--rename"] != "" {
		return 0, usagef("hooks add takes no --no-urgent, --clear-destination or --rename")
	}
	if err := hookOptions(p, stdin, &v); err != nil {
		return 0, err
	}
	if v.Mode == wire.HookModeForward && v.Dest == "" {
		return 0, usagef("--kind forward needs --destination-file or --destination-stdin")
	}
	f, err := newAddressFile(path)
	if err != nil {
		return 0, err
	}
	b, err := slotCall(c, "POST", "/api/boss/hooks", v)
	return finishAddress(c, f, path, b, err, p.bool("--json"), out, "added the webhook "+v.Name+" (to "+v.Role+", "+v.Mode+")")
}

// hooksEdit changes the options given and keeps the rest.
func hooksEdit(c Config, p parsedArgs, name string, stdin io.Reader, out io.Writer) (int, error) {
	if p.values["--name"] != "" || p.values["--address-file"] != "" {
		return 0, usagef("hooks edit takes --rename, not --name, and no --address-file (see hooks rotate)")
	}
	if p.bool("--urgent") && p.bool("--no-urgent") {
		return 0, usagef("--urgent and --no-urgent together")
	}
	h, err := hookByName(c, name)
	if err != nil {
		return 0, err
	}
	v := wire.HookSave{Name: h.Name, Role: h.Role, Mode: h.Mode, Urgent: h.Urgent, WindowMin: h.WindowMin}
	if n := strings.TrimSpace(p.values["--rename"]); n != "" {
		v.Name = n
	}
	if r := strings.TrimSpace(p.values["--to"]); r != "" {
		v.Role = r
	}
	switch {
	case p.bool("--urgent"):
		v.Urgent = true
	case p.bool("--no-urgent"):
		v.Urgent = false
	}
	if err = hookOptions(p, stdin, &v); err != nil {
		return 0, err
	}
	if p.bool("--clear-destination") {
		if v.Dest != "" {
			return 0, usagef("--clear-destination with a new destination")
		}
		v.ClearDest = true
	}
	return bossSend(c, "PUT", "hooks/"+strconv.FormatInt(h.ID, 10), v, p.bool("--json"), out, "changed the webhook "+v.Name)
}

// hooksRotate makes a webhook's address again; the old one stops working.
func hooksRotate(c Config, p parsedArgs, name string, out io.Writer) (int, error) {
	path := p.values["--address-file"]
	if path == "" {
		return 0, usagef("hooks rotate requires --address-file")
	}
	if !confirmed(p, out, "make a new address for the webhook "+name+": the old one stops working") {
		return ExitUsage, nil
	}
	f, err := newAddressFile(path)
	if err != nil {
		return 0, err
	}
	h, err := hookByName(c, name)
	if err != nil {
		f.Close()
		os.Remove(path)
		return 0, err
	}
	b, err := slotCall(c, "POST", "/api/boss/hooks/"+strconv.FormatInt(h.ID, 10)+"/address", nil)
	return finishAddress(c, f, path, b, err, p.bool("--json"), out, "made a new address for the webhook "+name)
}

// hookOptions reads --kind, --window and the destination into v.
func hookOptions(p parsedArgs, stdin io.Reader, v *wire.HookSave) error {
	if k, ok := p.values["--kind"]; ok {
		if k != wire.HookModeMail && k != wire.HookModeForward {
			return usagef("--kind must be %s or %s", wire.HookModeMail, wire.HookModeForward)
		}
		v.Mode = k
	}
	if w, ok := p.values["--window"]; ok {
		n, err := windowMinutes(w)
		if err != nil {
			return err
		}
		v.WindowMin = n
	}
	file, fromStdin := p.values["--destination-file"], p.bool("--destination-stdin")
	if file != "" && fromStdin {
		return usagef("--destination-file and --destination-stdin together")
	}
	var raw []byte
	var err error
	switch {
	case file != "":
		if raw, err = os.ReadFile(file); err != nil {
			return errors.New("cannot read " + file)
		}
	case fromStdin:
		if raw, err = io.ReadAll(io.LimitReader(stdin, 8<<10)); err != nil {
			return errors.New("cannot read the destination from stdin")
		}
	default:
		return nil
	}
	if v.Dest = strings.TrimSpace(string(raw)); v.Dest == "" {
		return usagef("the original destination is empty")
	}
	return nil
}

// windowMinutes reads --window: a duration of whole minutes (10m, 1h) or a
// number of minutes.
func windowMinutes(s string) (int, error) {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Minute || d%time.Minute != 0 {
		return 0, usagef("--window must be whole minutes, e.g. 10m or 1h")
	}
	return int(d / time.Minute), nil
}

// hookByName finds a webhook in the global boss's list, which has IDs.
func hookByName(c Config, name string) (wire.Hook, error) {
	b, err := slotCall(c, "GET", "/api/boss/hooks", nil)
	if err != nil {
		return wire.Hook{}, err
	}
	var list []wire.Hook
	if err = json.Unmarshal(b, &list); err != nil {
		return wire.Hook{}, fmt.Errorf("unexpected response from server: %v", err)
	}
	for _, h := range list {
		if h.Name == name {
			return h, nil
		}
	}
	return wire.Hook{}, fmt.Errorf("no webhook named %s", name)
}

// newAddressFile makes path, which must not exist, owner-only.
func newAddressFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil, errors.New(path + " exists; the address goes to a new file only")
	}
	if err != nil {
		return nil, errors.New("cannot make " + path)
	}
	if err = ownerfile.RestrictToUser(path); err != nil {
		f.Close()
		os.Remove(path)
		return nil, errors.New("cannot make " + path + " owner-only")
	}
	return f, nil
}

// finishAddress writes the address of the server's answer (b, or the call's
// error) to f and says done: the hook's settings with --json, never the
// address. A failed call removes the file.
func finishAddress(c Config, f *os.File, path string, b []byte, callErr error, jsonOut bool, out io.Writer, done string) (int, error) {
	var created wire.HookCreated
	if callErr == nil {
		if err := json.Unmarshal(b, &created); err != nil || created.Token == "" {
			callErr = errors.New("unexpected response from server")
		}
	}
	if callErr != nil {
		f.Close()
		os.Remove(path)
		return 0, callErr
	}
	_, err := f.WriteString(endpoint(c, "/hook/"+created.Token) + "\n")
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return 0, errors.New("cannot write " + path + "; the webhook has an address nobody holds: make a new one with hooks rotate")
	}
	if jsonOut {
		j, _ := json.Marshal(created.Hook)
		_, err = fmt.Fprintln(out, string(j))
		return ExitOK, err
	}
	_, err = fmt.Fprintf(out, "%s; address written to %s\n", done, path)
	return ExitOK, err
}
