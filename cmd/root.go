package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "awst",
		Short:         "AWS toolkit (Go port)",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.AddCommand(newCredsCmd(defaultDeps()))
	root.AddCommand(newLoginCmd(defaultLoginDeps()))
	root.AddCommand(newConnectCmd(defaultConnectDeps()))
	sd := defaultSessionsDeps()
	root.AddCommand(newListCmd(sd))
	root.AddCommand(newKillCmd(sd))
	return root
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
