package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newKillCmd(d sessionsDeps) *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "kill <pid> [pid...]",
		Short: "Terminate active SSM sessions",
		Long: `Terminate active SSM sessions started by awst connect.

Pass one or more PIDs (find them via ` + "`awst list`" + `), or use --all to
terminate every active session-manager-plugin process on this host.
Each kill uses the local platform's process termination semantics.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return errors.New("cannot mix --all with explicit PIDs")
			}
			if !all && len(args) == 0 {
				return errors.New("specify one or more PIDs, or pass --all")
			}

			var pids []int
			if all {
				if d.scan == nil {
					return errors.New("--all needs a session scanner")
				}
				list, err := d.scan()
				if err != nil {
					return err
				}
				if len(list) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no active SSM sessions")
					return nil
				}
				for _, s := range list {
					pids = append(pids, s.PID)
				}
			} else {
				for _, a := range args {
					n, err := strconv.Atoi(a)
					if err != nil {
						return fmt.Errorf("invalid pid %q", a)
					}
					pids = append(pids, n)
				}
			}

			var errs []error
			for _, pid := range pids {
				if err := d.kill(pid); err != nil {
					errs = append(errs, fmt.Errorf("pid %d: %w", pid, err))
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "killed %d\n", pid)
			}
			return errors.Join(errs...)
		},
	}
	c.Flags().BoolVar(&all, "all", false, "Terminate every active SSM session on this host")
	return c
}
