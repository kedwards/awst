package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/withreach/aws-tools/internal/creds"
	"github.com/withreach/aws-tools/internal/paths"
)

type credsDeps struct {
	store           *creds.Store
	providerFactory func(ctx context.Context, profile, region string) (creds.Provider, error)
	now             func() time.Time
}

func defaultDeps() credsDeps {
	return credsDeps{
		store:           creds.NewStore(paths.CredsDir()),
		providerFactory: creds.NewSDKProvider,
		now:             time.Now,
	}
}

func newCredsCmd(d credsDeps) *cobra.Command {
	c := &cobra.Command{
		Use:   "creds",
		Short: "Manage AWS credentials per profile",
		Long: `Store, use, list, and clear AWS credentials per profile.

Examples:
  eval "$(awst creds store dev)"
  eval "$(awst creds use dev)"
  awst creds list
  awst creds clear dev`,
	}
	c.AddCommand(newCredsStoreCmd(d), newCredsUseCmd(d), newCredsListCmd(d), newCredsClearCmd(d))
	return c
}

func newCredsStoreCmd(d credsDeps) *cobra.Command {
	var region string
	c := &cobra.Command{
		Use:   "store <profile>",
		Short: "Resolve credentials for <profile> and persist to disk",
		Long: `Resolve credentials for <profile> via the AWS SDK default credential
chain and persist them to disk. Prints shell `+"`export`"+` statements
intended for eval:

  eval "$(awst creds store dev)"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			p, err := d.providerFactory(ctx, profile, region)
			if err != nil {
				return err
			}
			resolved, err := creds.Resolve(ctx, profile, p)
			if err != nil {
				return err
			}
			resolved.Region = region
			if err := d.store.Save(profile, resolved); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), creds.FormatExports(profile, resolved))
			return nil
		},
	}
	c.Flags().StringVarP(&region, "region", "r", "", "AWS region to associate with the stored credentials")
	return c
}

func newCredsUseCmd(d credsDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "use <profile>",
		Short: "Print export statements for stored <profile> credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]
			c, err := d.store.Load(profile)
			if err != nil {
				if errors.Is(err, creds.ErrProfileNotStored) {
					return fmt.Errorf("no stored credentials for profile %q (run: eval \"$(awst creds store %s)\")", profile, profile)
				}
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), creds.FormatExports(profile, c))
			return nil
		},
	}
}

func newCredsListCmd(d credsDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored credential profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := d.store.List()
			if err != nil {
				return err
			}
			if len(profiles) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No stored credentials found")
				return nil
			}
			now := d.now()
			for _, p := range profiles {
				fmt.Fprintf(cmd.OutOrStdout(), "  %-30s (stored %s)\n", p.Name, ageString(now.Sub(p.StoredAt)))
			}
			return nil
		},
	}
}

func newCredsClearCmd(d credsDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "clear [profile]",
		Short: "Remove stored credentials (all profiles if none given)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if err := d.store.DeleteAll(); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Cleared all stored credentials")
				return nil
			}
			profile := args[0]
			if err := d.store.Delete(profile); err != nil {
				if errors.Is(err, creds.ErrProfileNotStored) {
					return fmt.Errorf("no stored credentials for profile %q", profile)
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cleared credentials for profile %q\n", profile)
			return nil
		},
	}
}

func ageString(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
