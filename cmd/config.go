package cmd

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/regions"
)

// newConfigCmd backs `awst config`: it prints the paths and AWS settings
// the binary actually resolves at runtime. Unlike the bash version it does
// NOT enumerate logging/menu/cache env vars or check for aws/assume/rsync/
// fzf — the Go port carries none of those. What it shows is the real
// surface: where creds and SSO tokens live, where `awst run` looks for
// commands, and which AWS profile/region the SDK chain will pick up.
func newConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Show resolved awst paths and AWS settings",
		Long: `Show the paths and AWS settings awst resolves at runtime.

Paths marked (missing) do not exist yet — that is normal until the
corresponding command first writes to them. Override any path-deriving
location with its env var (AWST_CREDS_DIR, AWST_CMD_DIR,
AWST_RUN_CMD_BASE, AWST_RUN_CMD_USER); AWS profile/region come from the
standard AWS_PROFILE / AWS_REGION / AWS_DEFAULT_REGION chain.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			printConfig(cmd.OutOrStdout())
			return nil
		},
	}
	c.AddCommand(newConfigRegionsCmd())
	return c
}

// newConfigRegionsCmd backs `awst config regions`: list, add, and remove the
// regions offered by the interactive region picker. Until the user adds one,
// the picker uses a built-in default list.
func newConfigRegionsCmd() *cobra.Command {
	regionsCmd := &cobra.Command{
		Use:   "regions",
		Short: "List/edit the regions offered by the interactive region picker",
		Long: `List, add, and remove the AWS regions the interactive region picker
offers. With no user-configured regions the picker falls back to a built-in
default list; adding any region switches the picker to your list.

Stored at ~/.config/aws-tools/regions.config (override with AWST_REGIONS_FILE).

Examples:
  awst config regions
  awst config regions add us-west-2
  awst config regions remove eu-west-1`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			user, err := regions.Load(paths.RegionsFile())
			if err != nil {
				return err
			}
			list := user
			source := "configured"
			if len(list) == 0 {
				list = regions.Default
				source = "defaults (none configured)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Regions (%s):\n", source)
			for _, r := range list {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", r)
			}
			return nil
		},
	}

	add := &cobra.Command{
		Use:   "add <region>",
		Short: "Add a region to the picker list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			added, err := regions.Add(paths.RegionsFile(), args[0])
			if err != nil {
				return err
			}
			if added {
				fmt.Fprintf(cmd.OutOrStdout(), "Added %s\n", args[0])
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is already configured\n", args[0])
			}
			return nil
		},
	}

	remove := &cobra.Command{
		Use:     "remove <region>",
		Aliases: []string{"rm"},
		Short:   "Remove a region from the picker list",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			removed, err := regions.Remove(paths.RegionsFile(), args[0])
			if err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", args[0])
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s was not in the list\n", args[0])
			}
			return nil
		},
	}

	regionsCmd.AddCommand(add, remove)
	return regionsCmd
}

func printConfig(w io.Writer) {
	defaultCmd := paths.RunCommandsDir()

	fmt.Fprintf(w, "awst %s\n\n", version)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintln(tw, "Credentials")
	fmt.Fprintf(tw, "  Store\t%s\n", marked(paths.CredsDir()))
	fmt.Fprintf(tw, "  SSO cache\t%s\n", marked(paths.SSOCacheDir()))
	fmt.Fprintln(tw, "")

	fmt.Fprintln(tw, "Commands (awst run)")
	fmt.Fprintf(tw, "  Dir\t%s\n", marked(envOr("AWST_RUN_CMD_USER", defaultCmd)))
	fmt.Fprintln(tw, "")

	fmt.Fprintln(tw, "Regions (picker)")
	if user, _ := regions.Load(paths.RegionsFile()); len(user) > 0 {
		fmt.Fprintf(tw, "  Source\t%s (%d configured)\n", paths.RegionsFile(), len(user))
	} else {
		fmt.Fprintf(tw, "  Source\tdefaults (%d); configure with `awst config regions add`\n", len(regions.Default))
	}
	fmt.Fprintln(tw, "")

	fmt.Fprintln(tw, "AWS")
	fmt.Fprintf(tw, "  Config file\t%s\n", marked(paths.AWSConfigFile()))
	fmt.Fprintf(tw, "  Profile\t%s\n", orNotSet(os.Getenv("AWS_PROFILE")))
	fmt.Fprintf(tw, "  Region\t%s\n", orNotSet(envOr("AWS_REGION", os.Getenv("AWS_DEFAULT_REGION"))))

	tw.Flush()
}

// marked appends "(missing)" to a path that does not exist on disk.
func marked(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p + " (missing)"
	}
	return p
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func orNotSet(v string) string {
	if v == "" {
		return "(not set)"
	}
	return v
}
