package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func runShell(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newShellCmd())

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), err
}

func TestShellInit_Posix(t *testing.T) {
	out, err := runShell(t, "shell", "init")
	require.NoError(t, err)
	require.Contains(t, out, "awst() {")
	require.Contains(t, out, `eval "$(command awst login --export "$@")"`)
	require.Contains(t, out, `eval "$(command awst logout --export "$@")"`)
	// logout must NOT be a plain passthrough, or it can't clear the env vars.
	require.NotContains(t, out, "|logout|")
}

func TestShellInit_PowerShell(t *testing.T) {
	out, err := runShell(t, "shell", "init", "--powershell")
	require.NoError(t, err)
	require.Contains(t, out, "function awst")
	require.Contains(t, out, "Invoke-Expression")
	require.Contains(t, out, "login --export --shell powershell")
	require.Contains(t, out, "logout --export --shell powershell")
	// logout must be dropped from the passthrough list (else it can't clear env).
	require.NotContains(t, out, "'kill','logout'")
}
