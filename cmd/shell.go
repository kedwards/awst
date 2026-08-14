package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/paths"
)

// shellInitEnvVar is exported by the script `awst shell init` emits, so the
// binary can tell whether the wrapper function is loaded in the calling shell.
// Without that signal `awst login` cannot know its exports never reached the
// caller's environment, and a successful login looks like a silent no-op.
const shellInitEnvVar = "AWST_SHELL_INIT"

// posixShellFunc defines an awst() function that wraps the binary. Known
// subcommands pass straight through; a bare first arg (or `login`) is treated
// as a profile, run through `login --export`, and its stdout eval'd so the
// credential env vars land in the current shell — the assume <profile> UX.
// `logout` is likewise eval'd (via `logout --export`) so it clears those env
// vars from the current shell.
const posixShellFunc = `awst() {
  case "${1:-}" in
    creds|connect|console|exec|run|list|kill|config|sso|shell|completion|help|--help|-h|--version|-v|"")
      command awst "$@" ;;
    login)
      shift; eval "$(command awst login --export "$@")" ;;
    logout)
      shift; eval "$(command awst logout --export "$@")" ;;
    *)
      eval "$(command awst login --export "$@")" ;;
  esac
}
`

// fishShellFunc is the fish equivalent: passthrough for known subcommands,
// otherwise login --export piped through source so the env vars land in the
// current shell.
const fishShellFunc = `function awst
  set -l passthrough creds connect console exec run list kill config sso shell completion help --help -h --version -v
  if test (count $argv) -eq 0
    command awst $argv
    return
  end
  if contains -- $argv[1] $passthrough
    command awst $argv
    return
  end
  switch $argv[1]
    case login
      set -e argv[1]
      command awst login --export --shell fish $argv | source
    case logout
      set -e argv[1]
      command awst logout --export --shell fish $argv | source
    case '*'
      command awst login --export --shell fish $argv | source
  end
end
`

// powershellShellFunc is the PowerShell equivalent: passthrough for known
// subcommands, otherwise login --export piped through Invoke-Expression.
const powershellShellFunc = `function awst {
  $passthrough = 'creds','connect','console','exec','run','list','kill','config','sso','shell','completion','help','--help','-h','--version','-v'
  if ($args.Count -eq 0 -or $passthrough -contains $args[0]) {
    & (Get-Command awst -CommandType Application) @args
    return
  }
  $tail = if ($args.Count -gt 1) { $args[1..($args.Count-1)] } else { @() }
  if ($args[0] -eq 'logout') {
    & (Get-Command awst -CommandType Application) logout --export --shell powershell @tail | Invoke-Expression
    return
  }
  $rest = if ($args[0] -eq 'login') { $tail } else { $args }
  & (Get-Command awst -CommandType Application) login --export --shell powershell @rest | Invoke-Expression
}
`

// posixShellInit is the whole script `awst shell init` prints: the marker
// export followed by the wrapper. The marker carries the version that emitted
// it so a shell still running a function eval'd by an older binary can be
// told apart from a current one.
func posixShellInit(v string) string {
	return fmt.Sprintf("export %s='%s'\n%s", shellInitEnvVar, v, posixShellFunc)
}

func powershellShellInit(v string) string {
	return fmt.Sprintf("$env:%s = '%s'\n%s", shellInitEnvVar, v, powershellShellFunc)
}

func fishShellInit(v string) string {
	return fmt.Sprintf("set -gx %s '%s'\n%s", shellInitEnvVar, v, fishShellFunc)
}

// shellIntegration is what the marker tells us about the calling shell.
type shellIntegration struct {
	loaded  bool
	version string // version that emitted the loaded wrapper; may not be ours
}

// stale reports a wrapper loaded by a different build than the one running.
// Usually it means awst was upgraded and the shell hasn't been restarted.
func (s shellIntegration) stale() bool { return s.loaded && s.version != version }

// detectShellIntegration reads the marker. getenv is injectable for tests; nil
// means the real environment.
func detectShellIntegration(getenv func(string) string) shellIntegration {
	if getenv == nil {
		getenv = os.Getenv
	}
	v := strings.TrimSpace(getenv(shellInitEnvVar))
	return shellIntegration{loaded: v != "", version: v}
}

// shellTTY calls an optional terminal check, treating an unset one as "not a
// terminal" so tests and piped runs default to the quiet path.
func shellTTY(f func() bool) bool {
	if f == nil {
		return false
	}
	return f()
}

// shellSetupHint prints the two ways to get the wrapper in place: a one-shot
// eval for the shell in front of the user, and the permanent install.
func shellSetupHint(w io.Writer, oneShot string) {
	fmt.Fprintf(w, "  this shell only:  %s\n", oneShot)
	fmt.Fprintf(w, "  every new shell:  awst shell install\n")
}

func shellFlavor(getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	return filepath.Base(strings.TrimSpace(getenv("SHELL")))
}

func loginShellSetupHint(getenv func(string) string, profile string) string {
	name := shellFlavor(getenv)
	switch {
	case strings.Contains(name, "fish"):
		return fmt.Sprintf("awst login --export --shell fish %s | source", profile)
	case strings.Contains(name, "pwsh"), strings.Contains(name, "powershell"):
		return fmt.Sprintf("awst login --export --shell powershell %s | iex", profile)
	default:
		return fmt.Sprintf(`eval "$(awst login --export %s)"`, profile)
	}
}

func logoutShellSetupHint(getenv func(string) string) string {
	name := shellFlavor(getenv)
	switch {
	case strings.Contains(name, "fish"):
		return "awst logout --export --shell fish | source"
	case strings.Contains(name, "pwsh"), strings.Contains(name, "powershell"):
		return "awst logout --export --shell powershell | iex"
	default:
		return `eval "$(awst logout --export)"`
	}
}

func newShellCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "shell",
		Short: "Shell integration helpers",
	}
	c.AddCommand(newShellInitCmd())
	c.AddCommand(newShellInstallCmd())
	return c
}

func newShellInitCmd() *cobra.Command {
	var powershell bool
	var fish bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Print the awst shell function to eval in your rc file",
		Long: `Print a shell function named awst that wraps the binary so that
` + "`awst <profile>`" + ` logs in and sets the AWS credential env vars in the
current shell (the same UX as ` + "`assume <profile>`" + `). Known subcommands
pass through unchanged.

The script also exports ` + shellInitEnvVar + ` so awst can tell the wrapper is
loaded and warn instead of exiting quietly when it isn't.

` + "`awst shell install`" + ` writes the right line into your startup file for
you. To do it by hand:

  bash/zsh (~/.bashrc, ~/.zshrc):
    eval "$(awst shell init)"
  fish (~/.config/fish/config.fish):
    awst shell init --fish | source

  PowerShell ($PROFILE):
    awst shell init --powershell | Out-String | Invoke-Expression`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if fish && powershell {
				return errors.New("choose at most one of --fish or --powershell")
			}
			if fish {
				fmt.Fprint(cmd.OutOrStdout(), fishShellInit(version))
			} else if powershell {
				fmt.Fprint(cmd.OutOrStdout(), powershellShellInit(version))
			} else {
				fmt.Fprint(cmd.OutOrStdout(), posixShellInit(version))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&fish, "fish", false, "Emit a fish function instead of POSIX (bash/zsh)")
	c.Flags().BoolVar(&powershell, "powershell", false, "Emit a PowerShell function instead of POSIX (bash/zsh)")
	return c
}

// Delimiters around the block awst owns in a startup file, so install can
// recognise its own work and replace it without touching anything else.
const (
	shellInstallBegin = "# >>> awst shell integration >>>"
	shellInstallEnd   = "# <<< awst shell integration <<<"
)

// shellInitBlock is the managed block written into a startup file. It calls
// `awst shell init` at shell startup rather than pasting the function body, so
// upgrading the binary upgrades the wrapper.
func shellInitBlock(powershell, fish bool) string {
	line := `eval "$(awst shell init)"`
	if fish {
		line = "awst shell init --fish | source"
	} else if powershell {
		line = "awst shell init --powershell | Out-String | Invoke-Expression"
	}
	return shellInstallBegin + "\n" + line + "\n" + shellInstallEnd + "\n"
}

func newShellInstallCmd() *cobra.Command {
	var powershell bool
	var fish bool
	var file string
	var print bool
	var force bool
	c := &cobra.Command{
		Use:   "install",
		Short: "Add the awst shell integration to your shell startup file",
		Long: `Write the ` + "`awst shell init`" + ` line into your shell startup file so the
awst() wrapper loads in every new shell. Without it, ` + "`awst login`" + ` can
cache an SSO token but cannot set credential env vars in your shell.

The target is picked from $SHELL (~/.bashrc for bash, ~/.zshrc for zsh,
~/.config/fish/config.fish for fish); override it with --file. PowerShell has
no discoverable profile path, so pass --file "$PROFILE" along with
--powershell.

The block awst writes is delimited by marker comments. Re-running is a no-op;
--force rewrites the block in place. --print shows the block without touching
any file.

Examples:
  awst shell install
  awst shell install --print
  awst shell install --file ~/.config/fish/config.fish
  awst shell install --fish
  awst shell install --powershell --file "$PROFILE"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if fish && powershell {
				return errors.New("choose at most one of --fish or --powershell")
			}
			installFish := fish || (!powershell && strings.Contains(shellFlavor(nil), "fish"))
			block := shellInitBlock(powershell, installFish)
			if print {
				fmt.Fprint(cmd.OutOrStdout(), block)
				return nil
			}

			path := file
			if path == "" {
				p, err := defaultRCFile(powershell, installFish)
				if err != nil {
					return err
				}
				path = p
			}

			written, msg, err := installShellBlock(path, block, force)
			if err != nil {
				return err
			}
			out := cmd.ErrOrStderr()
			fmt.Fprintln(out, msg)
			if written {
				if powershell {
					fmt.Fprintf(out, "Restart your shell or run: . %s\n", path)
				} else {
					fmt.Fprintf(out, "Restart your shell or run: source %s\n", path)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&fish, "fish", false, "Install the fish form instead of POSIX (bash/zsh)")
	c.Flags().BoolVar(&powershell, "powershell", false, "Install the PowerShell form instead of POSIX (bash/zsh)")
	c.Flags().StringVarP(&file, "file", "f", "", "Startup file to write (default: ~/.bashrc, ~/.zshrc, or ~/.config/fish/config.fish from $SHELL)")
	c.Flags().BoolVar(&print, "print", false, "Print the block that would be installed and exit")
	c.Flags().BoolVar(&force, "force", false, "Rewrite the block even if one is already present")
	return c
}

// defaultRCFile maps $SHELL to the startup file awst should write.
func defaultRCFile(powershell, fish bool) (string, error) {
	if powershell {
		return "", errors.New(`PowerShell's profile path is not discoverable from the environment; pass --file "$PROFILE"`)
	}
	home := paths.HomeDir()
	if home == "" {
		return "", errors.New("cannot determine your home directory; pass --file <path>")
	}
	if fish {
		return filepath.Join(home, ".config", "fish", "config.fish"), nil
	}
	name := filepath.Base(os.Getenv("SHELL"))
	switch {
	case strings.Contains(name, "zsh"):
		return filepath.Join(home, ".zshrc"), nil
	case strings.Contains(name, "bash"):
		return filepath.Join(home, ".bashrc"), nil
	case strings.Contains(name, "fish"):
		return filepath.Join(home, ".config", "fish", "config.fish"), nil
	case name == "" || name == "." || name == string(filepath.Separator):
		return "", errors.New("$SHELL is not set; pass --file <path>")
	default:
		return "", fmt.Errorf("no default startup file known for shell %q; pass --file <path>", name)
	}
}

// installShellBlock appends the managed block to path, creating the file if
// needed. It reports whether the file was written and a line describing what
// happened. An existing managed block, or a hand-written `awst shell init`
// line, is left alone unless force is set.
func installShellBlock(path, block string, force bool) (bool, string, error) {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, "", fmt.Errorf("read %s: %w", path, err)
	}
	content := string(b)

	switch {
	case strings.Contains(content, shellInstallBegin):
		if !force {
			return false, fmt.Sprintf("awst shell integration is already installed in %s", path), nil
		}
		content, err = stripShellBlock(content)
		if err != nil {
			return false, "", fmt.Errorf("rewrite %s: %w", path, err)
		}
	case strings.Contains(content, "awst shell init"):
		if !force {
			return false, fmt.Sprintf("%s already has an `awst shell init` line that awst did not write; leaving it alone (--force to add the managed block anyway)", path), nil
		}
	}

	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += block

	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, "", fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return false, "", fmt.Errorf("write %s: %w", path, err)
	}
	return true, fmt.Sprintf("Installed awst shell integration in %s", path), nil
}

// stripShellBlock removes the delimited block, including a blank line left
// dangling ahead of it, so repeated --force runs don't grow the file.
func stripShellBlock(content string) (string, error) {
	lines := strings.Split(content, "\n")
	var out []string
	skipping := false
	sawBegin := false
	sawEnd := false
	for _, line := range lines {
		switch {
		case strings.TrimSpace(line) == shellInstallBegin:
			if skipping {
				return "", errors.New("managed shell integration block is malformed: nested opening marker")
			}
			if len(out) > 0 && out[len(out)-1] == "" {
				out = out[:len(out)-1]
			}
			sawBegin = true
			skipping = true
			continue
		case strings.TrimSpace(line) == shellInstallEnd:
			if !skipping {
				return "", errors.New("managed shell integration block is malformed: closing marker without opening marker")
			}
			skipping = false
			sawEnd = true
			continue
		}
		if !skipping {
			out = append(out, line)
		}
	}
	if sawBegin && !sawEnd {
		return "", errors.New("managed shell integration block is missing its closing marker")
	}
	return strings.Join(out, "\n"), nil
}
