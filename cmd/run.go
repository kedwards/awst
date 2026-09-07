package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/runner"
	"github.com/kedwards/awst/v3/internal/tui"
)

type runDeps struct {
	resolveCreds   func(ctx context.Context, profile, region string) ([]string, error)
	resolveTargets func(ctx context.Context) ([]runner.Target, error) // used when no filter is given
	ensureLogin    func(ctx context.Context, errOut io.Writer, profile string) error
	runChild       func(args []string, env []string, stdout, stderr io.Writer) (int, error)
	shell          func() (string, error) // POSIX shell for snippets/inline
	isTerminal     func() bool
	selectCommand  func(names []string) (string, error)
}

func defaultRunDeps() runDeps {
	return runDeps{
		resolveCreds: defaultResolveCreds,
		resolveTargets: func(ctx context.Context) ([]runner.Target, error) {
			return resolveTargetsInteractive(ctx, isStdinTerminal, defaultListProfiles,
				tui.SelectProfiles, regionsEffective, tui.SelectRegionFor)
		},
		ensureLogin: func(ctx context.Context, errOut io.Writer, profile string) error {
			return defaultSSOLogin().ensure(ctx, errOut, profile, false)
		},
		runChild:      defaultRunChild,
		shell:         runner.POSIXShell,
		isTerminal:    isStdinTerminal,
		selectCommand: selectSavedCommand,
	}
}

func newRunCmd(d runDeps) *cobra.Command {
	var src cmdSource
	var profile, region, targets string
	c := &cobra.Command{
		Use:   "run [flags] [name] [filter]",
		Short: "Run a command across one or more AWS profiles",
		Long: `Run a command across one or more AWS profiles. For each target awst
resolves credentials via the SDK chain, exports AWS_PROFILE / AWS_REGION
/ AWS_ACCESS_KEY_ID / etc. into the child environment, and execs the
command.

The command body comes from exactly one of: --command/-c (inline),
--file/-f (a path), or a saved command name (positional, resolved from
the commands directory) — the same three sources, in the same order of
precedence, as "awst exec". With none of those, a terminal shows a
picker of saved commands; a pipe/CI prints the list and exits non-zero.
Use --list/-l to just print the list.

A saved command file is a plain script whose leading '# key: value'
comments (description/profile/region) set defaults — explicit flags
always win over them. Everything after the header is run verbatim via
sh -c, so heredocs, comments, and blank lines survive. Saved commands
live under ~/.config/aws-tools/commands/aws (override with
AWST_RUN_CMD_BASE / AWST_RUN_CMD_USER, or -d / AWST_CMD_DIR for an
exclusive override).

Targets come from --targets/-t (or the equivalent trailing positional),
from --profile/-p and --region/-r, or from a picker. A target filter is
comma- and/or space-separated "profile" or "profile:region" tokens and
is the only way to run against several profiles at once (bare "profile"
tokens default to us-east-1); passing more than one of -t, the
positional filter, and -p is an error. Use -t rather than the
positional when you want to name the targets but still pick the command
interactively. With no targets at all, a terminal multi-selects the
profiles and then picks a region for each, while a pipe/CI errors
instead of guessing.

Executable command files (+x) are exec'd directly instead of via sh -c,
and with no targets at all they run once without profile iteration —
the script is expected to handle its own iteration.

Examples:
  awst run                                     # pick a saved command
  awst run -l                                  # list saved commands
  awst run vpc-cidrs                           # pick profiles + regions
  awst run vpc-cidrs "dev prod:us-west-2"      # filtered
  awst run -t "dev prod:us-west-2"             # filtered, pick the command
  awst run vpc-cidrs -p dev -r us-west-2       # single target
  awst run -c "aws s3 ls" "dev,prod:us-west-2" # inline command
  awst run -d ./snippets my-snippet "dev"      # custom commands dir`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var filter string
			if len(args) > 0 {
				src.name = args[0]
			}
			if len(args) > 1 {
				filter = args[1]
			}
			// -c/-f take the name slot, so the lone positional is the filter.
			if (src.inline != "" || src.file != "") && src.name != "" {
				if filter != "" {
					return errors.New("too many arguments: with --command/-c or --file/-f the only positional is the filter")
				}
				filter, src.name = src.name, ""
			}
			if targets != "" {
				if filter != "" {
					return errors.New("specify the target filter with either --targets/-t or the positional, not both")
				}
				filter = targets
			}
			if filter != "" && profile != "" {
				return errors.New("specify targets with either the target filter or --profile/-p, not both")
			}

			dirs, dirErr := resolveCommandDirs(src.dir, "AWST_RUN", paths.RunCommandsDir())
			if src.list {
				if dirErr != nil {
					return dirErr
				}
				return listCommands(cmd.OutOrStdout(), dirs)
			}
			// A missing commands dir only matters once we need to resolve a
			// saved name; -c and -f work without one.
			if dirErr != nil && src.inline == "" && src.file == "" {
				return dirErr
			}

			script, err := resolveCommandSource(cmd.OutOrStdout(), src, dirs, d.isTerminal, d.selectCommand)
			if err != nil {
				if errors.Is(err, tui.ErrAborted) {
					return nil
				}
				return err
			}

			// Executable files are exec'd directly; everything else is a body
			// for sh -c.
			isExecutable := false
			if script.Path != "" {
				if info, statErr := os.Stat(script.Path); statErr == nil {
					isExecutable = info.Mode().Perm()&0o111 != 0
				}
			}
			if !isExecutable && strings.TrimSpace(script.Body) == "" {
				return errors.New("empty command body")
			}

			// Flags win; the saved script's header supplies defaults.
			profile = firstNonEmpty(profile, script.Profile)
			region = firstNonEmpty(region, script.Region)

			// Executable + no targets → single run, no profile iteration.
			if isExecutable && filter == "" && profile == "" {
				_, err := d.runChild([]string{script.Path}, os.Environ(), cmd.OutOrStdout(), cmd.ErrOrStderr())
				return err
			}

			// Bodies are POSIX shell; resolve sh once up front so every child
			// run uses the same interpreter.
			shell := ""
			if !isExecutable {
				shell, err = d.shell()
				if err != nil {
					return err
				}
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			targets, err := buildTargets(ctx, cmd.ErrOrStderr(), filter, profile, region, d.isTerminal, d.resolveTargets)
			if err != nil {
				if errors.Is(err, tui.ErrAborted) {
					return nil // user quit the picker; clean no-op exit
				}
				return err
			}
			if len(targets) == 0 {
				return errors.New("no profiles to run against")
			}

			var failed []string
			for _, t := range targets {
				fmt.Fprintln(cmd.OutOrStdout(), t.Profile)
				if d.ensureLogin != nil {
					if err := d.ensureLogin(ctx, cmd.ErrOrStderr(), t.Profile); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(),
							"  skip %s (%s): %v\n", t.Profile, t.Region, err)
						failed = append(failed, t.Profile)
						continue
					}
				}
				creds, err := d.resolveCreds(ctx, t.Profile, t.Region)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"  skip %s (%s): %v\n", t.Profile, t.Region, err)
					failed = append(failed, t.Profile)
					continue
				}
				env := append(os.Environ(),
					"AWS_PROFILE="+t.Profile,
					"AWS_REGION="+t.Region,
					"AWS_DEFAULT_REGION="+t.Region,
				)
				env = append(env, creds...)

				childArgs := []string{shell, "-c", script.Body}
				if isExecutable {
					childArgs = []string{script.Path}
				}
				if _, err := d.runChild(childArgs, env, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"  %s exited non-zero: %v\n", t.Profile, err)
					failed = append(failed, t.Profile)
				}
			}
			if len(failed) > 0 {
				return fmt.Errorf("command failed on %d profile(s): %s",
					len(failed), strings.Join(failed, ", "))
			}
			return nil
		},
	}
	addCommandSourceFlags(c, &src)
	c.Flags().StringVarP(&targets, "targets", "t", "", `Comma-separated "profile" or "profile:region" tokens (same as the positional filter)`)
	c.Flags().StringVarP(&profile, "profile", "p", "", "AWS profile to run against (instead of a target filter)")
	c.Flags().StringVarP(&region, "region", "r", "", "AWS region for --profile (defaults to the profile's region)")
	return c
}

func listCommands(w io.Writer, dirs []string) error {
	cmds, err := runner.List(dirs)
	if err != nil {
		return err
	}
	if len(cmds) == 0 {
		fmt.Fprintln(w, "No commands found.")
		return nil
	}
	fmt.Fprintln(w, "Available commands:")
	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	hasExec := false
	for _, c := range cmds {
		mark := ""
		if c.Executable {
			mark = "*"
			hasExec = true
		}
		fmt.Fprintf(tw, "  %s%s\t%s\n", c.Name, mark, c.Desc)
	}
	tw.Flush()
	if hasExec {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  * = executable script")
	}
	return nil
}

// buildTargets resolves what to run against, in precedence order: the
// positional filter (the only multi-target form), then --profile/-p (or a
// script header's profile:), then an interactive picker.
func buildTargets(ctx context.Context, w io.Writer, filter, profile, region string,
	isTerminal func() bool,
	resolveTargets func(context.Context) ([]runner.Target, error)) ([]runner.Target, error) {
	if filter != "" {
		return runner.ParseFilter(filter)
	}
	if profile != "" {
		p, r, err := resolveProfileRegion(ctx, w, profile, region, isTerminal)
		if err != nil {
			return nil, err
		}
		if r == "" {
			r = runner.DefaultRegion
		}
		return []runner.Target{{Profile: p, Region: r}}, nil
	}
	// No explicit targets: prompt for them (never fan out across every profile).
	return resolveTargets(ctx)
}

// defaultResolveCreds uses the SDK chain for a profile and returns its
// AWS_* credentials as env-var KEY=VALUE strings ready to splice into a
// child env. AWS_REGION is set by the caller.
func defaultResolveCreds(ctx context.Context, profile, region string) ([]string, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithSharedConfigProfile(profile),
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	c, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{
		"AWS_ACCESS_KEY_ID=" + c.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + c.SecretAccessKey,
	}
	if c.SessionToken != "" {
		out = append(out, "AWS_SESSION_TOKEN="+c.SessionToken)
	}
	return out, nil
}

// defaultListProfiles parses ~/.aws/config for profile names.
// Honors AWS_CONFIG_FILE env override. It is a variable so tests can stub it.
var defaultListProfiles = func() ([]string, error) {
	path := os.Getenv("AWS_CONFIG_FILE")
	if path == "" {
		path = paths.AWSConfigFile()
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var out []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
			continue
		}
		body := strings.TrimSpace(line[1 : len(line)-1])
		switch {
		case body == "default":
			out = append(out, "default")
		case strings.HasPrefix(body, "profile "):
			out = append(out, strings.TrimSpace(strings.TrimPrefix(body, "profile ")))
		}
	}
	return out, s.Err()
}

// defaultRunChild execs args[0] with args[1:], inheriting stdin and piping
// stdout/stderr to the given writers. Returns the child's exit code (0 on
// success).
func defaultRunChild(args []string, env []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 0, errors.New("no command to run")
	}
	c := exec.Command(args[0], args[1:]...)
	c.Env = env
	c.Stdin = os.Stdin
	c.Stdout = stdout
	c.Stderr = stderr
	err := c.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), err
		}
		return -1, err
	}
	return 0, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
