package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/sandcorporation/switchboard/internal/wire"
)

// dispatchIgnoringFailure runs a command whose own outcome does not matter
// here: it may fail for want of a profile or of a function the binary
// supplies, which the cli tests leave nil.
func dispatchIgnoringFailure(args []string) {
	defer func() { _ = recover() }()
	_, _ = dispatch(args)
}

// Every command not listed in noAutoPlugin keeps the plugin at the CLI's
// version on its first run, whichever branch of dispatch it takes (#485:
// host split off before the sync and was missed).
func TestEveryCommandOutsideNoAutoPluginSyncsThePlugin(t *testing.T) {
	for _, cmd := range commands {
		if noAutoPlugin[cmd.name] {
			continue
		}
		t.Run(cmd.name, func(t *testing.T) {
			t.Setenv("SWITCHBOARD_CONFIG_DIR", t.TempDir())
			t.Setenv("DB_PATH", t.TempDir()+"/none.db")
			installs := 0
			saved, savedFlag := binary, noPluginFlag
			t.Cleanup(func() { binary, noPluginFlag = saved, savedFlag })
			noPluginFlag = false
			binary.PluginInstall = func(version, dir string, quiet bool, w io.Writer) error {
				installs++
				return nil
			}
			args := []string{cmd.name}
			for i := 0; i < cmd.nargs; i++ {
				args = append(args, "x")
			}
			dispatchIgnoringFailure(args)
			if installs != 1 {
				t.Fatalf("%s: plugin installed %d times, want 1", strings.Join(args, " "), installs)
			}
		})
	}
}

// host's --profile is the hosted session's profile; the sync is not told it.
func TestHostSyncHonoursNoPlugin(t *testing.T) {
	t.Setenv("SWITCHBOARD_CONFIG_DIR", t.TempDir())
	t.Setenv("DB_PATH", t.TempDir()+"/none.db")
	installs := 0
	saved, savedFlag := binary, noPluginFlag
	t.Cleanup(func() { binary, noPluginFlag = saved, savedFlag })
	noPluginFlag = false
	binary.PluginInstall = func(version, dir string, quiet bool, w io.Writer) error {
		installs++
		return nil
	}
	binary.Host = func(action, id string, add wire.HostedSessionCreate, jsonOut bool, stdout io.Writer) (int, error) {
		return ExitOK, nil
	}
	dispatchIgnoringFailure([]string{"--no-plugin", "host", "list"})
	t.Setenv("SWITCHBOARD_PLUGIN", "off")
	noPluginFlag = false
	dispatchIgnoringFailure([]string{"host", "list"})
	if installs != 0 {
		t.Fatalf("installed %d times with the plugin off", installs)
	}
}
