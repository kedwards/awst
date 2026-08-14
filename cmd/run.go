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
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/runner"
	"github.com/kedwards/awst/v3/internal/sso"
	"github.com/kedwards/awst/v3/internal/tui"
)

type runDeps struct {
	resolveCreds   func(ctx context.Context, profile, region string) ([]string, error)
	resolveTargets func(ctx context.Context) ([]runner.Target, error) // used when no filter is given
	ensureLogin    func(ctx context.Context, errOut io.Writer, profile string) error
	runChild       func(args []string, env []string, stdout, stderr io.Writer) (int, error)
	getenv         func(string) string
	shell          func() (string, error) // POSIX shell for snippets/inline
}

func defaultRunDeps() runDeps {
	defaultBase := paths.RunCommandsDir()
	login := ssoLogin{
		cache:         sso.NewCache(paths.SSOCacheDir()),
		sessionLoader: sso.LoadSSOSession,
		oidcFactory: func(ctx context.Context, region string) (sso.OIDCClient, error) {
			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
			if err != nil {
				return nil, fmt.Errorf("load aws config: %w", err)
			}
			return ssooidc.NewFromConfig(cfg), nil
		},
		openBrowser: openBrowser,
		sleep:       time.Sleep,
		now:         time.Now,
	}
	return runDeps{
		resolveCreds: defaultResolveCreds,
		resolveTargets: func(ctx context.Context) ([]runner.Target, error) {
			return resolveTargetsInteractive(ctx, isStdinTerminal, defaultListProfiles,
				tui.SelectProfiles, regionsEffective, tui.SelectRegionFor)
		},
		ensureLogin: func(ctx context.Context, errOut io.Writer, profile string) error {
			return login.ensure(ctx, errOut, profile, false)
		},
		runChild: defaultRunChild,
		shell:    runner.POSIXShell,
		getenv: func(k string) string {
			v := os.Getenv(k)
			if v != "" {
				return v
			}
			if k == "AWST_RUN_CMD_BASE" || k == "AWST_RUN_CMD_USER" {
				return defaultBase
			}
			return ""
		},
	}
}

func newRunCmd(d runDeps) *cobra.Command {
	var query, customDir string
	c := &cobra.Command{
		Use:   "run [flags] [name] [filter]",
		Short: "Run a command or snippet across one or more AWS profiles",
		Long: `Run a command or snippet across one or more AWS profiles.

Snippets and executable scripts are discovered under the commands
directory (default ~/.config/aws-tools/commands/aws). Snippet
placeholders #ENV and #REGION are substituted to the current profile
and region; AWS_PROFILE / AWS_REGION / AWS_ACCESS_KEY_ID / etc. are
also exported into the child process environment, so new snippets can
just reference $AWS_PROFILE directly.

Filter syntax (positional): comma- and/or space-separated "profile" or
"profile:region" tokens. No filter → an interactive picker: multi-select
the profiles, then choose a region for each (bare "profile" tokens default
to us-east-1). With no filter and no terminal (pipe/CI), run errors instead
of guessing.

Executable scripts with no filter run once without profile iteration —
the script is expected to handle its own iteration.

Examples:
  awst run                                     # list available commands
  awst run vpc-cidrs                           # pick profiles + regions
  awst run vpc-cidrs "dev prod:us-west-2"      # filtered
  awst run -q "aws s3 ls" "dev,prod:us-west-2" # inline command
  awst run -d ./snippets my-snippet "dev"      # custom commands dir`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, err := runner.ResolveDirs(runner.Options{
				D:    firstNonEmpty(customDir, d.getenv("AWST_CMD_DIR")),
				Base: d.getenv("AWST_RUN_CMD_BASE"),
				User: d.getenv("AWST_RUN_CMD_USER"),
			})
			if err != nil {
				// List/help still work with no dirs configured; only error
				// when actually trying to resolve a command. Fall through.
				if query == "" && len(args) == 0 {
					return err
				}
				if query == "" {
					return err
				}
				// -q doesn't need dirs; allow it to proceed with empty dirs.
				dirs = nil
			}

			// Inline -q is treated as if the user passed it as the command name.
			var name, filter string
			switch {
			case query != "":
				name = query
				if len(args) > 0 {
					filter = args[0]
				}
			case len(args) == 0:
				return listCommands(cmd.OutOrStdout(), dirs)
			default:
				name = args[0]
				if len(args) > 1 {
					filter = args[1]
				}
			}

			scriptPath := ""
			isExecutable := false
			isInline := query != ""
			if !isInline {
				p, err := runner.ResolveScript(name, dirs)
				if err != nil {
					return err
				}
				scriptPath = p
				if info, statErr := os.Stat(scriptPath); statErr == nil {
					isExecutable = info.Mode().Perm()&0o111 != 0
				}
			}

			// Executable + no filter → single run, no profile iteration.
			if isExecutable && filter == "" {
				_, err := d.runChild([]string{scriptPath}, os.Environ(), cmd.OutOrStdout(), cmd.ErrOrStderr())
				return err
			}

			// Determine the command text. Inline / snippet → sh -c text;
			// executable + filter → run script directly per profile.
			var snippet string
			if isInline {
				snippet = query
			} else if !isExecutable {
				body, err := runner.LoadSnippet(scriptPath)
				if err != nil {
					return err
				}
				snippet = body
			}

			// Snippets and inline commands are POSIX shell; resolve sh once
			// up front so every child run uses the same interpreter.
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

			targets, err := buildTargets(ctx, filter, d.resolveTargets)
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

				var childArgs []string
				if isExecutable {
					childArgs = []string{scriptPath}
				} else {
					childArgs = []string{shell, "-c", runner.Substitute(snippet, t.Profile, t.Region)}
				}
				if _, err := d.runChild(childArgs, env, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"  %s exited non-zero: %v\n", t.Profile, err)
				}
			}
			_ = failed // bash semantics: don't fail the parent on per-profile errors
			return nil
		},
	}
	c.Flags().StringVarP(&query, "query", "q", "", "Inline command to run (instead of a saved snippet)")
	c.Flags().StringVarP(&customDir, "dir", "d", "", "Commands directory (exclusive override)")
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

func buildTargets(ctx context.Context, filter string, resolveTargets func(context.Context) ([]runner.Target, error)) ([]runner.Target, error) {
	if filter != "" {
		return runner.ParseFilter(filter)
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
// Honors AWS_CONFIG_FILE env override.
func defaultListProfiles() ([]string, error) {
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
