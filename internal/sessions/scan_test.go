package sessions

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseArgs_StartSession(t *testing.T) {
	argv := []string{
		"/usr/local/bin/session-manager-plugin",
		`{"SessionId":"s1","TokenValue":"t","StreamUrl":"wss://x"}`,
		"us-east-1",
		"StartSession",
		"dev",
		`{"Target":"i-0123abc"}`,
		"https://ssm.us-east-1.amazonaws.com",
	}
	s, ok := ParseArgs(argv)
	require.True(t, ok)
	require.Equal(t, Session{
		Type:    "shell",
		Target:  "i-0123abc",
		Region:  "us-east-1",
		Profile: "dev",
	}, s)
}

func TestParseArgs_BareBinaryName(t *testing.T) {
	argv := []string{
		"session-manager-plugin",
		`{}`,
		"eu-west-2",
		"StartSession",
		"",
		`{"Target":"i-x"}`,
		"https://ssm.eu-west-2.amazonaws.com",
	}
	s, ok := ParseArgs(argv)
	require.True(t, ok)
	require.Equal(t, "eu-west-2", s.Region)
	require.Equal(t, "i-x", s.Target)
	require.Equal(t, "", s.Profile)
}

func TestParseArgs_PortForward(t *testing.T) {
	argv := []string{
		"session-manager-plugin",
		`{}`,
		"us-east-1",
		"StartPortForwardingSession",
		"dev",
		`{"Target":"i-port","Parameters":{"localPortNumber":["8080"]}}`,
		"https://ssm.us-east-1.amazonaws.com",
	}
	s, ok := ParseArgs(argv)
	require.True(t, ok)
	require.Equal(t, "port-forward", s.Type)
	require.Equal(t, "i-port", s.Target)
}

func TestParseArgs_NotPlugin(t *testing.T) {
	argv := []string{"/usr/bin/bash", "-c", "echo hi"}
	_, ok := ParseArgs(argv)
	require.False(t, ok)
}

func TestParseArgs_WrongLength(t *testing.T) {
	argv := []string{"session-manager-plugin", "only", "three", "args"}
	_, ok := ParseArgs(argv)
	require.False(t, ok)
}

func TestParseArgs_MalformedParamsJSON(t *testing.T) {
	argv := []string{
		"session-manager-plugin",
		`{}`,
		"us-east-1",
		"StartSession",
		"dev",
		`{not-json`,
		"https://ssm.us-east-1.amazonaws.com",
	}
	s, ok := ParseArgs(argv)
	require.True(t, ok, "should still report session even if Target unparseable")
	require.Equal(t, "", s.Target)
}

// fakeProcDir creates a /proc-like directory tree for testing Scan.
func fakeProcDir(t *testing.T, entries map[int][]string) string {
	t.Helper()
	root := t.TempDir()
	for pid, argv := range entries {
		pidDir := filepath.Join(root, strconv.Itoa(pid))
		require.NoError(t, os.MkdirAll(pidDir, 0o755))
		// /proc/PID/cmdline is NUL-separated argv with trailing NUL
		content := strings.Join(argv, "\x00") + "\x00"
		require.NoError(t, os.WriteFile(filepath.Join(pidDir, "cmdline"), []byte(content), 0o644))
	}
	return root
}

func TestScan_FindsPluginProcessesSkipsOthers(t *testing.T) {
	pluginArgv := []string{
		"/usr/local/bin/session-manager-plugin",
		`{}`,
		"us-east-1",
		"StartSession",
		"dev",
		`{"Target":"i-aaa"}`,
		"https://ssm.us-east-1.amazonaws.com",
	}
	root := fakeProcDir(t, map[int][]string{
		1234: pluginArgv,
		5678: {"/usr/bin/bash", "-c", "sleep 1"},
		9999: pluginArgv,
	})

	got, err := Scan(root)
	require.NoError(t, err)
	require.Len(t, got, 2)

	pids := []int{got[0].PID, got[1].PID}
	require.Contains(t, pids, 1234)
	require.Contains(t, pids, 9999)
	require.NotContains(t, pids, 5678)
}

func TestScan_EmptyProc(t *testing.T) {
	got, err := Scan(t.TempDir())
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestScan_NonExistentRoot(t *testing.T) {
	_, err := Scan("/no/such/path")
	require.Error(t, err)
}
