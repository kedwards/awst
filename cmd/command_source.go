package cmd

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/runner"
	"github.com/kedwards/awst/v3/internal/tui"
)

// cmdSource is the command-body surface shared by `awst exec` and `awst run`:
// exactly one of inline/file/name supplies the body, dir overrides where saved
// names are looked up, and list short-circuits to the command listing.
type cmdSource struct {
	inline string // -c/--command
	file   string // -f/--file
	name   string // positional [name]
	dir    string // -d/--dir
	list   bool   // -l/--list
}

// addCommandSourceFlags registers the flags both commands share. Keeping the
// registration in one place is what makes "-c means the same thing on both"
// true by construction rather than by review.
func addCommandSourceFlags(c *cobra.Command, s *cmdSource) {
	c.Flags().StringVarP(&s.inline, "command", "c", "", "Inline command to run")
	c.Flags().StringVarP(&s.file, "file", "f", "", "Path to a command file (body with an optional '# key: value' header)")
	c.Flags().StringVarP(&s.dir, "dir", "d", "", "Commands directory (exclusive override; also AWST_CMD_DIR)")
	c.Flags().BoolVarP(&s.list, "list", "l", false, "List the available saved commands and exit")
}

// resolveCommandDirs layers the commands directories for one command. -d (or
// AWST_CMD_DIR) is an exclusive override; otherwise $<prefix>_CMD_BASE then
// $<prefix>_CMD_USER, each defaulting to defaultDir.
func resolveCommandDirs(custom, envPrefix, defaultDir string) ([]string, error) {
	env := func(suffix string) string {
		if v := os.Getenv(envPrefix + suffix); v != "" {
			return v
		}
		return defaultDir
	}
	return runner.ResolveDirs(runner.Options{
		D:    firstNonEmpty(custom, os.Getenv("AWST_CMD_DIR")),
		Base: env("_CMD_BASE"),
		User: env("_CMD_USER"),
	})
}

// errNoCommand is returned when no body was given and we couldn't prompt for
// one. Callers print the command listing alongside it.
var errNoCommand = errors.New("no command given; pass --command/-c, --file/-f, or a saved command name")

// resolveCommandSource turns the flags into the script to run. Inline bodies
// come back as a bare Script; files and saved names are loaded through
// runner.Load so the header/verbatim-body format is identical either way. With
// no source at all, a terminal gets a picker and a pipe/CI gets the listing
// plus errNoCommand.
func resolveCommandSource(out io.Writer, s cmdSource, dirs []string,
	isTerminal func() bool, pick func([]string) (string, error)) (runner.Script, error) {
	sources := 0
	for _, v := range []string{s.inline, s.file, s.name} {
		if strings.TrimSpace(v) != "" {
			sources++
		}
	}
	if sources > 1 {
		return runner.Script{}, errors.New("specify the command with only one of --command/-c, --file/-f, or a saved command name")
	}

	switch {
	case strings.TrimSpace(s.inline) != "":
		return runner.Script{Body: s.inline}, nil
	case s.file != "":
		return runner.Load(s.file)
	case s.name != "":
		p, err := runner.ResolveScript(s.name, dirs)
		if err != nil {
			return runner.Script{}, err
		}
		return runner.Load(p)
	}

	cmds, err := runner.List(dirs)
	if err != nil {
		return runner.Script{}, err
	}
	if len(cmds) == 0 {
		return runner.Script{}, errors.New("no command given and no saved commands found; pass --command/-c or --file/-f")
	}
	if isTerminal == nil || !isTerminal() {
		_ = listCommands(out, dirs)
		return runner.Script{}, errNoCommand
	}
	names := make([]string, len(cmds))
	for i, c := range cmds {
		names[i] = c.Name
	}
	chosen, err := pick(names)
	if err != nil {
		return runner.Script{}, err // may be tui.ErrAborted
	}
	p, err := runner.ResolveScript(chosen, dirs)
	if err != nil {
		return runner.Script{}, err
	}
	return runner.Load(p)
}

// selectSavedCommand is the default picker for resolveCommandSource.
func selectSavedCommand(names []string) (string, error) {
	return tui.SelectFrom("Select a saved command to run", names)
}
