package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/kedwards/aws-tools/internal/sessions"
)

// sessionsDeps is shared by list and kill: both look up the local plugin
// processes and the kill command also sends signals.
type sessionsDeps struct {
	scan func() ([]sessions.Session, error)
	kill func(pid int) error
}

func defaultSessionsDeps() sessionsDeps {
	return sessionsDeps{
		scan: sessions.DefaultScan,
		kill: sessions.Kill,
	}
}

func newListCmd(d sessionsDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List active SSM sessions on this host",
		Long: `List active SSM sessions started by awst connect (or the AWS CLI) on
this host. Uses the local platform's process inspection to find running
session-manager-plugin processes and pulls region / profile / target
from their argv.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := d.scan()
			if err != nil {
				return err
			}
			printSessions(cmd.OutOrStdout(), list)
			return nil
		},
	}
}

func printSessions(w io.Writer, list []sessions.Session) {
	if len(list) == 0 {
		fmt.Fprintln(w, "no active SSM sessions")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PID\tTYPE\tINSTANCE\tREGION\tPROFILE")
	for _, s := range list {
		target := s.Target
		if target == "" {
			target = "-"
		}
		profile := s.Profile
		if profile == "" {
			profile = "-"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", s.PID, s.Type, target, s.Region, profile)
	}
	tw.Flush()
}
