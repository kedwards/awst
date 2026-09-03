package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/connect"
	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/runner"
	"github.com/kedwards/awst/v3/internal/ssmexec"
	"github.com/kedwards/awst/v3/internal/tui"
)

type execDeps struct {
	clients        func(ctx context.Context, profile, region string) (*ssmClients, error)
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
		sleep:      time.Sleep,
		isTerminal: isStdinTerminal,
		selectCommand: func(names []string) (string, error) {
			return tui.SelectFrom("Select a saved command to run", names)
		},
		selectInstance: tui.SelectInstance,
	}
}

// resolveExecDirs returns the commands dir(s) `awst exec` resolves a saved
// command name against: customDir (the -d flag) is an exclusive override;
// otherwise AWST_EXEC_CMD_DIR, falling back to paths.ExecCommandsDir().
func resolveExecDirs(customDir string) ([]string, error) {
	base := os.Getenv("AWST_EXEC_CMD_DIR")
	if base == "" {
		base = paths.ExecCommandsDir()
	}
	return runner.ResolveDirs(runner.Options{D: customDir, Base: base})
}

// loadScriptByName resolves name against dirs and loads it as a Script.
func loadScriptByName(name string, dirs []string) (ssmexec.Script, error) {
	p, err := runner.ResolveScript(name, dirs)
	if err != nil {
		return ssmexec.Script{}, err
	}
	return ssmexec.Load(p)
}

func newExecCmd(d execDeps) *cobra.Command {
	var profile, region, command, instances, file, dir string
	c := &cobra.Command{
		Use:   "exec [name] [flags]",
		Short: "Run a shell command on one or more SSM-managed instances",
		Long: `Run a shell command via ssm:SendCommand on one or more SSM-managed
EC2 instances. <instances> is a comma-separated mix of Name-tag substring
patterns and i-… IDs; each pattern is expanded against the live SSM
inventory and a no-match is a hard error (no silent partial runs). Omit
-i to pick an instance interactively (errors in a pipe/CI).

The command body comes from exactly one of: --command/-c (inline),
--file/-f (a path), or a saved command name (positional, resolved from
the commands directory). With none of those, a terminal shows a picker
of saved commands; a pipe/CI prints the list and exits non-zero.

A saved command file is a plain script whose leading '# key: value'
comments (description/profile/region/instances) set defaults — explicit
flags always win over them. Everything after the header, including
heredocs and blank lines, is sent verbatim. Saved commands live under
~/.config/aws-tools/commands/ssm (override with AWST_EXEC_CMD_DIR or -d).

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
  awst exec                                  # pick a saved command interactively`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			sources := 0
			for _, s := range []string{command, file, name} {
				if strings.TrimSpace(s) != "" {
					sources++
				}
			}
			if sources > 1 {
				return errors.New("specify the command with only one of --command/-c, --file/-f, or a saved command name")
			}

			var script ssmexec.Script
			haveScript := false
			var err error
			switch {
			case strings.TrimSpace(command) != "":
				// inline command; nothing to load
			case file != "":
				if script, err = ssmexec.Load(file); err != nil {
					return err
				}
				haveScript = true
			case name != "":
				dirs, dirErr := resolveExecDirs(dir)
				if dirErr != nil {
					return dirErr
				}
				if script, err = loadScriptByName(name, dirs); err != nil {
					return err
				}
				haveScript = true
			default:
				dirs, dirErr := resolveExecDirs(dir)
				if dirErr != nil {
					return errors.New("missing --command/-c, --file/-f, or a saved command name")
				}
				cmds, err := runner.List(dirs)
				if err != nil {
					return err
				}
				if len(cmds) == 0 {
					return errors.New("missing --command/-c, --file/-f, or a saved command name (no saved commands found)")
				}
				if !d.isTerminal() {
					_ = listCommands(cmd.OutOrStdout(), dirs)
					return errors.New("no command given; pass --command/-c, --file/-f, a saved command name, or run interactively to pick one")
				}
				names := make([]string, len(cmds))
				for i, sc := range cmds {
					names[i] = sc.Name
				}
				chosen, err := d.selectCommand(names)
				if err != nil {
					if errors.Is(err, tui.ErrAborted) {
						return nil
					}
					return err
				}
				if script, err = loadScriptByName(chosen, dirs); err != nil {
					return err
				}
				haveScript = true
			}

			body := command
			scriptName := ""
			if haveScript {
				body = script.Body
				scriptName = script.Name
			}
			if strings.TrimSpace(body) == "" {
				return errors.New("empty command body")
			}

			// Flags win; the saved script's header supplies defaults.
			if profile == "" && haveScript {
				profile = script.Profile
			}
			if region == "" && haveScript {
				region = script.Region
			}
			if instances == "" && haveScript {
				instances = script.Instances
			}

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
			if scriptName != "" {
				banner = fmt.Sprintf("Running %q on %d instance(s) in %s...", scriptName, len(ids), clients.Region)
			}
			fmt.Fprintln(cmd.ErrOrStderr(), banner)

			results, err := ssmexec.Run(ctx, clients.Cmd, body, ids, d.sleep)
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
	c.Flags().StringVarP(&command, "command", "c", "", "Inline shell command to execute")
	c.Flags().StringVarP(&file, "file", "f", "", "Path to a command file (script body with an optional '# key: value' header)")
	c.Flags().StringVarP(&dir, "dir", "d", "", "Commands directory (default ~/.config/aws-tools/commands/ssm; override AWST_EXEC_CMD_DIR)")
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
