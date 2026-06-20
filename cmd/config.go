package cmd

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/kedwards/awst/internal/paths"
)

// newConfigCmd backs `awst config`: it prints the paths and AWS settings
// the binary actually resolves at runtime. Unlike the bash version it does
// NOT enumerate logging/menu/cache env vars or check for aws/assume/rsync/
// fzf — the Go port carries none of those. What it shows is the real
// surface: where creds and SSO tokens live, where `awst run` looks for
// commands, and which AWS profile/region the SDK chain will pick up.
func newConfigCmd() *cobra.Command {
	return &cobra.Command{
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
