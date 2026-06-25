package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/connect"
	"github.com/kedwards/awst/v3/internal/ssmexec"
	"github.com/kedwards/awst/v3/internal/tui"
)

type execDeps struct {
	clients func(ctx context.Context, profile, region string) (*ssmClients, error)
	sleep   func(time.Duration)
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
		sleep: time.Sleep,
	}
}

func newExecCmd(d execDeps) *cobra.Command {
	var profile, region, command, instances string
	c := &cobra.Command{
		Use:   "exec -c <command> -i <instances>",
		Short: "Run a shell command on one or more SSM-managed instances",
		Long: `Run a shell command via ssm:SendCommand on one or more SSM-managed
EC2 instances. <instances> is a comma-separated mix of Name-tag substring
patterns and i-… IDs; each pattern is expanded against the live SSM
inventory and a no-match is a hard error (no silent partial runs).

The command runs under AWS-RunShellScript (default /bin/sh — include
your own shebang or wrap with bash -c if you need bash features). stdout
is the first 24 KB, stderr the first 8 KB; larger output needs S3 (not
configured by this command yet).

Examples:
  awst exec -c 'uptime' -i web-1
  awst exec -c 'df -h' -i web,db,i-0123abc
  awst exec -c 'systemctl restart nginx' -i web -p prod -r us-east-2`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(command) == "" {
				return errors.New("missing --command/-c")
			}
			if strings.TrimSpace(instances) == "" {
				return errors.New("missing --instances/-i (comma-separated names or i-… IDs)")
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			// Resolve profile/region, prompting with a picker when missing and
			// interactive (skips the region prompt when already resolvable).
			var err error
			profile, region, err = resolveProfileRegion(ctx, profile, region, isStdinTerminal)
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

			fmt.Fprintf(cmd.ErrOrStderr(),
				"Running on %d instance(s) in %s...\n", len(ids), clients.Region)

			results, err := ssmexec.Run(ctx, clients.Cmd, command, ids, d.sleep)
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
	c.Flags().StringVarP(&command, "command", "c", "", "Shell command to execute (required)")
	c.Flags().StringVarP(&instances, "instances", "i", "", "Comma-separated instance names or i-… IDs (required)")
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
