package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/connect"
	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/ssmexec"
	"github.com/kedwards/awst/v3/internal/tui"
)

type execDeps struct {
	clients        func(ctx context.Context, profile, region string) (*ssmClients, error)
	ensureLogin    func(ctx context.Context, errOut io.Writer, profile string) error
	sleep          func(time.Duration)
	isTerminal     func() bool
	selectCommand  func(names []string) (string, error)
	selectInstance func(items []tui.InstanceItem) (string, error)
}

func defaultExecDeps() execDeps {
	return execDeps{
		clients: func(ctx context.Context, profile, region string) (*ssmClients, error) {
			opts := []func(*config.LoadOptions) error{}
			if profile != "" {
				opts = append(opts, config.WithSharedConfigProfile(profile))
			}
			if region != "" {
				opts = append(opts, config.WithRegion(region))
			}
			cfg, err := config.LoadDefaultConfig(ctx, opts...)
			if err != nil {
				return nil, fmt.Errorf("load aws config: %w", err)
			}
			ssmClient := ssm.NewFromConfig(cfg)
			return &ssmClients{
				SSM:        ssmClient,
				EC2:        ec2.NewFromConfig(cfg),
				SSMSession: ssmClient,
				Cmd:        ssmClient,
				Region:     cfg.Region,
				Profile:    profile,
			}, nil
		},
		ensureLogin: func(ctx context.Context, errOut io.Writer, profile string) error {
			return defaultSSOLogin().ensure(ctx, errOut, profile, false)
		},
		sleep:          time.Sleep,
		isTerminal:     isStdinTerminal,
		selectCommand:  selectSavedCommand,
		selectInstance: tui.SelectInstance,
	}
}

func newExecCmd(d execDeps) *cobra.Command {
	var src cmdSource
	var profile, region, instances string
	c := &cobra.Command{
		Use:   "exec [flags] [name]",
		Short: "Run a shell command on one or more SSM-managed instances",
		Long: `Run a shell command via ssm:SendCommand on one or more SSM-managed
EC2 instances. <instances> is a comma-separated mix of Name-tag substring
patterns and i-… IDs; each pattern is expanded against the live SSM
inventory and a no-match is a hard error (no silent partial runs). Omit
-i to pick an instance interactively (errors in a pipe/CI).

The command body comes from exactly one of: --command/-c (inline),
--file/-f (a path), or a saved command name (positional, resolved from
the commands directory) — the same three sources, in the same order of
precedence, as "awst run". With none of those, a terminal shows a picker
of saved commands; a pipe/CI prints the list and exits non-zero. Use
--list/-l to just print the list.

A saved command file is a plain script whose leading '# key: value'
comments (description/profile/region/instances) set defaults — explicit
flags always win over them. Everything after the header, including
heredocs and blank lines, is sent verbatim. Saved commands live under
~/.config/aws-tools/commands/ssm (override with AWST_EXEC_CMD_BASE /
AWST_EXEC_CMD_USER, or -d / AWST_CMD_DIR for an exclusive override).

The command runs under AWS-RunShellScript (default /bin/sh — include
your own shebang or wrap with bash -c if you need bash features). stdout
is the first 24 KB, stderr the first 8 KB; larger output needs S3 (not
configured by this command yet).

Examples:
  awst exec -c 'uptime' -i web-1
  awst exec -c 'df -h' -i web,db,i-0123abc
  awst exec -c 'systemctl restart nginx' -i web -p prod -r us-east-2
  awst exec barx-rate-check                  # saved command, header sets profile/region/instances
  awst exec -f ./check.sh -i web-1           # ad-hoc file
  awst exec                                  # pick a saved command interactively
  awst exec -l                               # list saved commands`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				src.name = args[0]
			}
			dirs, dirErr := resolveCommandDirs(src.dir, "AWST_EXEC", paths.ExecCommandsDir())
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
			if strings.TrimSpace(script.Body) == "" {
				return errors.New("empty command body")
			}

			// Flags win; the saved script's header supplies defaults.
			profile = firstNonEmpty(profile, script.Profile)
			region = firstNonEmpty(region, script.Region)
			instances = firstNonEmpty(instances, script.Instances)

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			// Resolve profile/region, prompting with a picker when missing and
			// interactive (skips the region prompt when already resolvable).
			profile, region, err = resolveProfileRegion(ctx, cmd.ErrOrStderr(), profile, region, d.isTerminal)
			if err != nil {
				if errors.Is(err, tui.ErrAborted) {
					return nil
				}
				return err
			}

			if d.ensureLogin != nil {
				if err := d.ensureLogin(ctx, cmd.ErrOrStderr(), profile); err != nil {
					return err
				}
			}

			clients, err := d.clients(ctx, profile, region)
			if err != nil {
				return err
			}

			list, err := connect.List(ctx, clients.SSM, clients.EC2)
			if err != nil {
				return authHint(err, clients.Profile)
			}

			if strings.TrimSpace(instances) == "" {
				if !d.isTerminal() {
					return errors.New("missing --instances/-i (comma-separated names or i-… IDs)")
				}
				if len(list) == 0 {
					return errors.New("no SSM-managed instances found in this account/region")
				}
				id, err := d.selectInstance(toInstanceItems(list))
				if err != nil {
					if errors.Is(err, tui.ErrAborted) {
						return nil
					}
					return err
				}
				instances = id
			}

			targets, err := ssmexec.Expand(instances, list)
			if err != nil {
				return err
			}
			ids := make([]string, 0, len(targets))
			nameByID := map[string]string{}
			for _, t := range targets {
				ids = append(ids, t.ID)
				nameByID[t.ID] = t.Name
			}

			banner := fmt.Sprintf("Running on %d instance(s) in %s...", len(ids), clients.Region)
			if script.Name != "" {
				banner = fmt.Sprintf("Running %q on %d instance(s) in %s...", script.Name, len(ids), clients.Region)
			}
			fmt.Fprintln(cmd.ErrOrStderr(), banner)

			results, err := ssmexec.Run(ctx, clients.Cmd, script.Body, ids, d.sleep)
			if err != nil {
				return authHint(err, clients.Profile)
			}

			out := cmd.OutOrStdout()
			var failed []string
			for _, r := range results {
				printResult(out, r, nameByID[r.InstanceID])
				if r.Failed() {
					failed = append(failed, r.InstanceID)
				}
			}
			if len(failed) > 0 {
				return fmt.Errorf("command failed on %d instance(s): %s",
					len(failed), strings.Join(failed, ", "))
			}
			return nil
		},
	}
	addCommandSourceFlags(c, &src)
	c.Flags().StringVarP(&instances, "instances", "i", "", "Comma-separated instance names or i-… IDs (prompts interactively if omitted)")
	c.Flags().StringVarP(&profile, "profile", "p", "", "AWS profile (defaults to SDK chain)")
	c.Flags().StringVarP(&region, "region", "r", "", "AWS region (defaults to SDK config)")
	return c
}

func printResult(w interface{ Write([]byte) (int, error) }, r ssmexec.Result, name string) {
	label := r.InstanceID
	if name != "" {
		label = fmt.Sprintf("%s (%s)", r.InstanceID, name)
	}
	fmt.Fprintf(w, "=== %s [%s exit=%d] ===\n", label, r.Status, r.ExitCode)
	if r.Stdout != "" {
		fmt.Fprint(w, r.Stdout)
		if !strings.HasSuffix(r.Stdout, "\n") {
			fmt.Fprintln(w)
		}
	}
	if r.Stderr != "" {
		fmt.Fprintln(w, "--- stderr ---")
		fmt.Fprint(w, r.Stderr)
		if !strings.HasSuffix(r.Stderr, "\n") {
			fmt.Fprintln(w)
		}
	}
	fmt.Fprintln(w)
}
