package cmd

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kedwards/awst/v3/internal/sessions"
)

type killRecorder struct {
	mu   sync.Mutex
	pids []int
	err  error
}

func (k *killRecorder) kill(pid int) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.pids = append(k.pids, pid)
	return k.err
}

func runKill(t *testing.T, d sessionsDeps, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newKillCmd(d))
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func TestKill_RequiresPidsOrAll(t *testing.T) {
	r := &killRecorder{}
	d := sessionsDeps{kill: r.kill}
	_, _, err := runKill(t, d, "kill")
	require.Error(t, err)
	require.Empty(t, r.pids)
}

func TestKill_ByPID(t *testing.T) {
	r := &killRecorder{}
	d := sessionsDeps{kill: r.kill}
	_, _, err := runKill(t, d, "kill", "1234", "5678")
	require.NoError(t, err)
	require.Equal(t, []int{1234, 5678}, r.pids)
}

func TestKill_NonNumericPIDRejected(t *testing.T) {
	r := &killRecorder{}
	d := sessionsDeps{kill: r.kill}
	_, _, err := runKill(t, d, "kill", "not-a-pid")
	require.Error(t, err)
	require.Empty(t, r.pids)
}

func TestKill_All_TargetsScannedSessions(t *testing.T) {
	r := &killRecorder{}
	d := sessionsDeps{
		scan: func() ([]sessions.Session, error) {
			return []sessions.Session{
				{PID: 100, Target: "i-a"},
				{PID: 200, Target: "i-b"},
			}, nil
		},
		kill: r.kill,
	}
	_, _, err := runKill(t, d, "kill", "--all")
	require.NoError(t, err)
	require.Equal(t, []int{100, 200}, r.pids)
}

func TestKill_All_NoSessions(t *testing.T) {
	r := &killRecorder{}
	d := sessionsDeps{
		scan: func() ([]sessions.Session, error) { return nil, nil },
		kill: r.kill,
	}
	out, _, err := runKill(t, d, "kill", "--all")
	require.NoError(t, err)
	require.Empty(t, r.pids)
	require.Contains(t, out, "no active")
}

func TestKill_All_PidsRejected(t *testing.T) {
	d := sessionsDeps{kill: (&killRecorder{}).kill}
	_, _, err := runKill(t, d, "kill", "--all", "123")
	require.Error(t, err)
}

func TestKill_AggregatesErrors(t *testing.T) {
	r := &killRecorder{err: errors.New("boom")}
	d := sessionsDeps{
		scan: func() ([]sessions.Session, error) {
			return []sessions.Session{{PID: 100}, {PID: 200}}, nil
		},
		kill: r.kill,
	}
	_, _, err := runKill(t, d, "kill", "--all")
	require.Error(t, err)
	require.Equal(t, []int{100, 200}, r.pids, "should attempt both even after first fails")
}

func TestKill_HelpFlag(t *testing.T) {
	d := sessionsDeps{kill: (&killRecorder{}).kill}
	out, _, err := runKill(t, d, "kill", "-h")
	require.NoError(t, err)
	require.Contains(t, out, "kill")
	require.Contains(t, out, "--all")
}
