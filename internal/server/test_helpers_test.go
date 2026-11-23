package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/sandcorporation/switchboard/internal/adminauth"
	"github.com/sandcorporation/switchboard/internal/cli"
	"github.com/sandcorporation/switchboard/internal/wire"
)

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(i int64) any {
	if i == 0 {
		return nil
	}
	return i
}

// Limits and names the tests check, as a client sees them.
const (
	maxPermissionTool = 128 // bytes
	maxPermissionText = 500 // bytes, for summary and note
	ndjsonMimeType    = "application/x-ndjson"
)

// discordPayload is the webhook body a Discord notice posts.
type discordPayload struct {
	Content         string `json:"content"`
	Username        string `json:"username,omitempty"`
	AvatarURL       string `json:"avatar_url,omitempty"`
	AllowedMentions struct {
		Parse []string `json:"parse"`
	} `json:"allowed_mentions"`
}

// binaryCommands are the commands cmd/switchboard hands the client.
func binaryCommands() cli.Commands {
	return cli.Commands{
		Admin:     adminauth.CLI,
		Host:      HostCLI,
		HostAuto:  HostAutoCLI,
		HostGrant: HostGrantCLI,
		Up:        func(dir string, port int, node string) error { return Up(version, dir, port, node) },
		Node: func(host, tokenFile, joinCode string, restartSessions, allowChecks bool) error {
			return Node(version, host, tokenFile, joinCode, restartSessions, allowChecks)
		},
		NodeInvite:     NodeInviteCLI,
		NodeStatusLine: NodeStatusLineCLI,
		// Never the real install: a line per call in SWITCHBOARD_TEST_PLUGIN_LOG.
		PluginInstall: func(version, dir string, quiet bool, w io.Writer) error {
			return testPluginCall("install", dir, quiet, w)
		},
		PluginStatus: func(version, dir string, jsonOut bool, sync wire.PluginSync, w io.Writer) error {
			if err := testPluginCall("status", dir, jsonOut, w); err != nil {
				return err
			}
			b, _ := json.Marshal(sync)
			_, err := fmt.Fprintf(w, "sync %s\n", b)
			return err
		},
	}
}

// testPluginCall records a plugin call for the CLI tests (#477): action,
// whether the profiles in dir existed then, and the flag; the installer
// prints a changed line, or fails, as SWITCHBOARD_TEST_PLUGIN_RESULT says.
func testPluginCall(action, dir string, flag bool, w io.Writer) error {
	log := os.Getenv("SWITCHBOARD_TEST_PLUGIN_LOG")
	if log == "" {
		return nil
	}
	profiles, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "%s profiles=%d flag=%v\n", action, len(profiles), flag)
	switch os.Getenv("SWITCHBOARD_TEST_PLUGIN_RESULT") {
	case "fail":
		fmt.Fprintln(w, "plugin: fake: install failed: boom")
		return errors.New("plugin install: boom")
	case "changed":
		fmt.Fprintln(w, "plugin: fake: installed fake 1.0.0 (changed /cfg)")
	}
	return nil
}

// runMain is cmd/switchboard's main: the server without arguments, else the
// client.
func runMain() {
	args := append([]string(nil), os.Args[1:]...)
	if len(args) == 0 {
		Run(version)
		return
	}
	v := version
	if os.Getenv("SWITCHBOARD_TEST_PLUGIN_LOG") != "" {
		v = "v1.2.3" // a release build: only those sync the plugin (#498)
	}
	os.Exit(cli.Run(args, v, binaryCommands()))
}
