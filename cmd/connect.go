package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"text/tabwriter"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/spf13/cobra"

	"github.com/kedwards/aws-tools/internal/connect"
)

type ssmClients struct {
	SSM        connect.SSMClient
	EC2        connect.EC2Client
	SSMSession connect.SSMSessionClient
	Region     string
	Profile    string
}

type connectDeps struct {
	clients    func(ctx context.Context, profile, region string) (*ssmClients, error)
	runner     connect.PluginRunner
	lookPlugin func() error
}

func defaultConnectDeps() connectDeps {
	pluginBin := connect.PluginName
	if v := os.Getenv("AWST_SSM_PLUGIN"); v != "" {
		pluginBin = v
	}
	return connectDeps{
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
				Region:     cfg.Region,
				Profile:    profile,
			}, nil
		},
		runner: connect.ExecRunner{Binary: pluginBin},
		lookPlugin: func() error {
			_, err := exec.LookPath(pluginBin)
			return err
		},
	}
}

func newConnectCmd(d connectDeps) *cobra.Command {
	var profile, region string
	c := &cobra.Command{
		Use:   "connect [instance]",
		Short: "Open an SSM shell session on an EC2 instance",
		Long: `Open an SSM shell session on an SSM-managed EC2 instance.

If [instance] starts with "i-", it's treated as an instance ID. Otherwise
it's matched as a case-insensitive substring against the Name tag. If no
arg is given, the matching is ambiguous, or nothing matches, the list of
SSM-managed instances is printed and the command exits non-zero.

Requires session-manager-plugin on PATH (override with AWST_SSM_PLUGIN).

Examples:
  awst connect i-0123abc
  awst connect web-prod
  awst connect              # lists available instances`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := d.lookPlugin(); err != nil {
				return fmt.Errorf("session-manager-plugin not found on PATH (override with AWST_SSM_PLUGIN): %w", err)
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			clients, err := d.clients(ctx, profile, region)
			if err != nil {
				return err
			}

			list, err := connect.List(ctx, clients.SSM, clients.EC2)
			if err != nil {
				return authHint(err, clients.Profile)
			}

			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			matches := connect.Resolve(arg, list)

			switch {
			case len(matches) == 0 && arg == "":
				printInstances(cmd.OutOrStdout(), list)
				return errors.New("no SSM-managed instances found in this account/region")
			case len(matches) == 0:
				printInstances(cmd.OutOrStdout(), list)
				return fmt.Errorf("no instance matched %q", arg)
			case len(matches) > 1:
				printInstances(cmd.OutOrStdout(), matches)
				return fmt.Errorf("ambiguous: %d instances matched %q (refine the pattern or pass an i-… id)", len(matches), arg)
			}

			inst := matches[0]
			fmt.Fprintf(cmd.ErrOrStderr(), "Connecting to %s (%s) in %s...\n", inst.Name, inst.ID, clients.Region)
			return authHint(connect.StartSession(ctx, clients.SSMSession, d.runner,
				inst.ID, clients.Region, clients.Profile, connect.SSMEndpoint(clients.Region)), clients.Profile)
		},
	}
	c.Flags().StringVarP(&profile, "profile", "p", "", "AWS profile (defaults to SDK chain)")
	c.Flags().StringVarP(&region, "region", "r", "", "AWS region (defaults to SDK config)")
	return c
}

func printInstances(w io.Writer, list []connect.Instance) {
	if len(list) == 0 {
		fmt.Fprintln(w, "(no instances)")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tID\tSTATE\tPING")
	for _, i := range list {
		name := i.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, i.ID, i.State, i.Ping)
	}
	tw.Flush()
}
