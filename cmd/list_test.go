package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kedwards/awst/v3/internal/sessions"
)

func writeFakeProc(t *testing.T, entries map[int][]string) string {
	t.Helper()
	root := t.TempDir()
	for pid, argv := range entries {
		dir := filepath.Join(root, strconv.Itoa(pid))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		content := strings.Join(argv, "\x00") + "\x00"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "cmdline"), []byte(content), 0o644))
	}
	return root
}

func runList(t *testing.T, d sessionsDeps, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newListCmd(d))
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func TestList_NoSessions(t *testing.T) {
	procRoot := writeFakeProc(t, nil)
	d := sessionsDeps{scan: func() ([]sessions.Session, error) {
		return sessions.Scan(procRoot)
	}}
	out, _, err := runList(t, d, "list")
	require.NoError(t, err)
	require.Contains(t, out, "no active")
}

func TestList_RendersTable(t *testing.T) {
	procRoot := writeFakeProc(t, map[int][]string{
		1234: {
			"session-manager-plugin",
			`{}`,
			"us-east-1",
			"StartSession",
			"dev",
			`{"Target":"i-aaa"}`,
			"https://ssm.us-east-1.amazonaws.com",
		},
	})
	d := sessionsDeps{scan: func() ([]sessions.Session, error) {
		return sessions.Scan(procRoot)
	}}
	out, _, err := runList(t, d, "list")
	require.NoError(t, err)
	require.Contains(t, out, "PID")
	require.Contains(t, out, "TYPE")
	require.Contains(t, out, "INSTANCE")
	require.Contains(t, out, "REGION")
	require.Contains(t, out, "PROFILE")
	require.Contains(t, out, "1234")
	require.Contains(t, out, "i-aaa")
	require.Contains(t, out, "us-east-1")
	require.Contains(t, out, "dev")
	require.Contains(t, out, "shell")
}

func TestList_HelpFlag(t *testing.T) {
	d := sessionsDeps{}
	out, _, err := runList(t, d, "list", "-h")
	require.NoError(t, err)
	require.Contains(t, out, "list")
}
