package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type childCall struct {
	args   []string
	env    map[string]string
	stdout string
}

type childRecorder struct {
	calls    []childCall
	stdouts  map[string]string // optional canned stdout per match-on-args[0]
	errProf  map[string]error  // optional canned error per AWS_PROFILE env var
	exitCode map[string]int
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		m[kv[:i]] = kv[i+1:]
	}
	return m
}

func (r *childRecorder) run(args []string, env []string, stdout, _ io.Writer) (int, error) {
	em := envMap(env)
	call := childCall{args: args, env: em}
	prof := em["AWS_PROFILE"]
	if out, ok := r.stdouts[args[0]]; ok {
		_, _ = stdout.Write([]byte(out))
		call.stdout = out
	} else if prof != "" {
		_, _ = stdout.Write([]byte(prof + " ran " + strings.Join(args, " ") + "\n"))
	}
	r.calls = append(r.calls, call)
	if err, ok := r.errProf[prof]; ok && err != nil {
		return 1, err
	}
	return r.exitCode[prof], nil
}

func runRunCmd(t *testing.T, d runDeps, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newRunCmd(d))
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func newTestDeps(t *testing.T, baseDir string, child *childRecorder) runDeps {
	t.Helper()
	return runDeps{
		resolveCreds: func(_ context.Context, profile, _ string) ([]string, error) {
			return []string{
				"AWS_ACCESS_KEY_ID=AKIA-" + profile,
				"AWS_SECRET_ACCESS_KEY=secret-" + profile,
				"AWS_SESSION_TOKEN=token-" + profile,
			}, nil
		},
		listProfiles: func() ([]string, error) { return []string{"dev", "prod"}, nil },
		runChild:     child.run,
		shell:        func() (string, error) { return "sh", nil },
		getenv: func(k string) string {
			if k == "AWST_RUN_CMD_BASE" || k == "AWST_RUN_CMD_USER" {
				return baseDir
			}
			return ""
		},
	}
}

func TestRun_ListsCommandsWhenNoArgs(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "vpc-cidrs", "#!/bin/sh\n# Show VPC CIDRs\naws ec2 describe-vpcs\n", false)
	writeFileT(t, d, "instances", "#!/bin/sh\n# List instances\naws ec2 describe-instances\n", true)

	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	out, _, err := runRunCmd(t, deps, "run", "-d", d)
	require.NoError(t, err)
	require.Contains(t, out, "vpc-cidrs")
	require.Contains(t, out, "Show VPC CIDRs")
	require.Contains(t, out, "instances")
	if runtime.GOOS != "windows" {
		// The "*" marks executables (Perm()&0o111); always 0 on windows.
		require.Contains(t, out, "*", "executables should be marked")
	}
	require.Empty(t, child.calls, "list mode should not invoke child")
}

func TestRun_SnippetExpandsAndIteratesAllProfiles(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "vpc-cidrs", "# header\naws ec2 describe-vpcs --region #REGION\n", false)

	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	out, _, err := runRunCmd(t, deps, "run", "-d", d, "vpc-cidrs")
	require.NoError(t, err)
	require.Contains(t, out, "dev")
	require.Contains(t, out, "prod")

	require.Len(t, child.calls, 2)
	require.Equal(t, []string{"sh", "-c"}, child.calls[0].args[:2], "snippets run via sh -c")
	require.Contains(t, child.calls[0].args[2], "--region us-east-1")
	require.Equal(t, "AKIA-dev", child.calls[0].env["AWS_ACCESS_KEY_ID"])
	require.Equal(t, "dev", child.calls[0].env["AWS_PROFILE"])
	require.Equal(t, "AKIA-prod", child.calls[1].env["AWS_ACCESS_KEY_ID"])
}

func TestRun_SnippetWithFilter(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "snippet", "echo #ENV in #REGION\n", false)

	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "snippet", "qa:us-west-2 ops")
	require.NoError(t, err)
	require.Len(t, child.calls, 2)
	require.Contains(t, child.calls[0].args[2], "echo qa in us-west-2")
	require.Contains(t, child.calls[1].args[2], "echo ops in us-east-1")
}

func TestRun_InlineCommand(t *testing.T) {
	d := t.TempDir()
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "-q", "aws s3 ls", "dev")
	require.NoError(t, err)
	require.Len(t, child.calls, 1)
	require.Equal(t, "aws s3 ls", child.calls[0].args[2])
	require.Equal(t, "dev", child.calls[0].env["AWS_PROFILE"])
}

func TestRun_NoPOSIXShell_ErrorsClearly(t *testing.T) {
	// Mirrors Windows without Git Bash/WSL: snippets can't run.
	d := t.TempDir()
	writeFileT(t, d, "snip", "aws s3 ls\n", false)
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)
	deps.shell = func() (string, error) { return "", errors.New("no POSIX shell on PATH") }

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "snip", "dev")
	require.Error(t, err)
	require.Contains(t, err.Error(), "POSIX shell")
	require.Empty(t, child.calls, "nothing runs without a shell")
}

func TestRun_ExecutableNoFilter_RunsOnceWithoutIteration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit detection is unix-only (windows support is the P2 run-semantics gap)")
	}
	d := t.TempDir()
	scriptPath := writeFileT(t, d, "self-iter", "#!/bin/sh\necho I handle iteration myself\n", true)
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "self-iter")
	require.NoError(t, err)
	require.Len(t, child.calls, 1, "executable + no filter → no profile loop")
	require.Equal(t, scriptPath, child.calls[0].args[0])
	require.NotContains(t, child.calls[0].env, "AWS_ACCESS_KEY_ID", "no creds injected when no profile loop")
}

func TestRun_ExecutableWithFilter_IteratesPerProfile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit detection is unix-only (windows support is the P2 run-semantics gap)")
	}
	d := t.TempDir()
	scriptPath := writeFileT(t, d, "per-profile", "#!/bin/sh\necho hi\n", true)
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "per-profile", "dev prod")
	require.NoError(t, err)
	require.Len(t, child.calls, 2)
	for _, c := range child.calls {
		require.Equal(t, scriptPath, c.args[0])
		require.NotEmpty(t, c.env["AWS_ACCESS_KEY_ID"])
	}
}

func TestRun_AuthFailure_WarnAndContinue(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "snippet", "echo #ENV\n", false)

	child := &childRecorder{}
	deps := newTestDeps(t, d, child)
	deps.resolveCreds = func(_ context.Context, profile, _ string) ([]string, error) {
		if profile == "dev" {
			return nil, errors.New("no valid SSO token")
		}
		return []string{"AWS_PROFILE=" + profile}, nil
	}

	_, stderr, err := runRunCmd(t, deps, "run", "-d", d, "snippet", "dev prod")
	require.NoError(t, err, "auth failure on one profile should not fail the command")
	require.Contains(t, stderr, "dev")
	require.Contains(t, stderr, "skip")
	require.Len(t, child.calls, 1, "only the successful profile runs")
	require.Equal(t, "prod", child.calls[0].env["AWS_PROFILE"])
}

func TestRun_UnknownCommand(t *testing.T) {
	d := t.TempDir()
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "ghost")
	require.Error(t, err)
}

func TestRun_HelpFlag(t *testing.T) {
	child := &childRecorder{}
	deps := newTestDeps(t, t.TempDir(), child)
	out, _, err := runRunCmd(t, deps, "run", "-h")
	require.NoError(t, err)
	require.Contains(t, out, "run")
	require.Contains(t, out, "-q")
	require.Contains(t, out, "-d")
}

func writeFileT(t *testing.T, dir, name, body string, exec bool) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, name)
	mode := os.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	require.NoError(t, os.WriteFile(p, []byte(body), mode))
	return p
}
