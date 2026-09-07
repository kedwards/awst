package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kedwards/awst/v3/internal/runner"
	"github.com/kedwards/awst/v3/internal/tui"
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

func newTestDeps(t *testing.T, _ string, child *childRecorder) runDeps {
	t.Helper()
	return runDeps{
		resolveCreds: func(_ context.Context, profile, _ string) ([]string, error) {
			return []string{
				"AWS_ACCESS_KEY_ID=AKIA-" + profile,
				"AWS_SECRET_ACCESS_KEY=secret-" + profile,
				"AWS_SESSION_TOKEN=token-" + profile,
			}, nil
		},
		// Default no-filter resolution for tests: two targets, no real picker.
		resolveTargets: func(_ context.Context) ([]runner.Target, error) {
			return []runner.Target{
				{Profile: "dev", Region: "us-east-1"},
				{Profile: "prod", Region: "us-east-1"},
			}, nil
		},
		ensureLogin:   func(_ context.Context, _ io.Writer, _ string) error { return nil },
		runChild:      child.run,
		shell:         func() (string, error) { return "sh", nil },
		isTerminal:    func() bool { return false },
		selectCommand: func([]string) (string, error) { return "", errors.New("picker not expected") },
	}
}

func TestRun_ListFlag(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "vpc-cidrs", "#!/bin/sh\n# Show VPC CIDRs\naws ec2 describe-vpcs\n", false)
	writeFileT(t, d, "instances", "#!/bin/sh\n# List instances\naws ec2 describe-instances\n", true)

	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	out, _, err := runRunCmd(t, deps, "run", "-d", d, "-l")
	require.NoError(t, err)
	require.Contains(t, out, "vpc-cidrs")
	require.Contains(t, out, "Show VPC CIDRs")
	require.Contains(t, out, "instances")
	require.Contains(t, out, "*", "executables should be marked")
	require.Empty(t, child.calls, "list mode should not invoke child")
}

func TestRun_NoSourceNonInteractive_ListsAndErrors(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "vpc-cidrs", "#!/bin/sh\n# Show VPC CIDRs\naws ec2 describe-vpcs\n", false)
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	out, _, err := runRunCmd(t, deps, "run", "-d", d)
	require.Error(t, err, "a pipe/CI must not guess a command")
	require.Contains(t, out, "vpc-cidrs", "the listing is still printed as a hint")
	require.Empty(t, child.calls)
}

func TestRun_NoSourceInteractive_PicksSavedCommand(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "vpc-cidrs", "aws ec2 describe-vpcs\n", false)
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)
	deps.isTerminal = func() bool { return true }
	deps.selectCommand = func(names []string) (string, error) {
		require.Equal(t, []string{"vpc-cidrs"}, names)
		return "vpc-cidrs", nil
	}

	_, _, err := runRunCmd(t, deps, "run", "-d", d)
	require.NoError(t, err)
	require.Len(t, child.calls, 2, "picked command runs against the resolved targets")
	require.Contains(t, child.calls[0].args[2], "aws ec2 describe-vpcs")
}

func TestRun_SnippetNoFilterUsesResolvedTargets(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "vpc-cidrs", "# header\naws ec2 describe-vpcs --region \"$AWS_REGION\"\n", false)

	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	out, _, err := runRunCmd(t, deps, "run", "-d", d, "vpc-cidrs")
	require.NoError(t, err)
	require.Contains(t, out, "dev")
	require.Contains(t, out, "prod")

	require.Len(t, child.calls, 2)
	require.Equal(t, []string{"sh", "-c"}, child.calls[0].args[:2], "bodies run via sh -c")
	require.Contains(t, child.calls[0].args[2], "# header", "body is verbatim, comments included")
	require.Contains(t, child.calls[0].args[2], `--region "$AWS_REGION"`)
	require.Equal(t, "us-east-1", child.calls[0].env["AWS_REGION"])
	require.Equal(t, "AKIA-dev", child.calls[0].env["AWS_ACCESS_KEY_ID"])
	require.Equal(t, "dev", child.calls[0].env["AWS_PROFILE"])
	require.Equal(t, "AKIA-prod", child.calls[1].env["AWS_ACCESS_KEY_ID"])
}

func TestRun_SnippetWithFilter(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "snippet", "echo \"$AWS_PROFILE in $AWS_REGION\"\n", false)

	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "snippet", "qa:us-west-2 ops")
	require.NoError(t, err)
	require.Len(t, child.calls, 2)
	require.Equal(t, "qa", child.calls[0].env["AWS_PROFILE"])
	require.Equal(t, "us-west-2", child.calls[0].env["AWS_REGION"])
	require.Equal(t, "ops", child.calls[1].env["AWS_PROFILE"])
	require.Equal(t, "us-east-1", child.calls[1].env["AWS_REGION"])
}

func TestRun_InlineCommand(t *testing.T) {
	d := t.TempDir()
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "-c", "aws s3 ls", "dev")
	require.NoError(t, err)
	require.Len(t, child.calls, 1)
	require.Equal(t, "aws s3 ls", child.calls[0].args[2])
	require.Equal(t, "dev", child.calls[0].env["AWS_PROFILE"])
}

func TestRun_NoPOSIXShell_ErrorsClearly(t *testing.T) {
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
	writeFileT(t, d, "snippet", "echo hi\n", false)

	child := &childRecorder{}
	deps := newTestDeps(t, d, child)
	deps.resolveCreds = func(_ context.Context, profile, _ string) ([]string, error) {
		if profile == "dev" {
			return nil, errors.New("no valid SSO token")
		}
		return []string{"AWS_PROFILE=" + profile}, nil
	}

	_, stderr, err := runRunCmd(t, deps, "run", "-d", d, "snippet", "dev prod")
	require.Error(t, err, "a failed profile makes the command exit non-zero")
	require.Contains(t, err.Error(), "dev")
	require.Contains(t, stderr, "dev")
	require.Contains(t, stderr, "skip")
	require.Len(t, child.calls, 1, "only the successful profile runs")
	require.Equal(t, "prod", child.calls[0].env["AWS_PROFILE"])
}

func TestRun_LoginFailure_SkipsProfile(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "snippet", "echo hi\n", false)

	child := &childRecorder{}
	deps := newTestDeps(t, d, child)
	deps.ensureLogin = func(_ context.Context, _ io.Writer, profile string) error {
		if profile == "dev" {
			return errors.New("device authorization declined")
		}
		return nil
	}

	_, stderr, err := runRunCmd(t, deps, "run", "-d", d, "snippet", "dev prod")
	require.Error(t, err)
	require.Contains(t, err.Error(), "dev")
	require.Contains(t, stderr, "skip")
	require.Len(t, child.calls, 1, "login failure skips that profile before running")
	require.Equal(t, "prod", child.calls[0].env["AWS_PROFILE"])
}

func TestRun_NoFilterAborted_ExitsClean(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "snippet", "echo hi\n", false)

	child := &childRecorder{}
	deps := newTestDeps(t, d, child)
	deps.resolveTargets = func(_ context.Context) ([]runner.Target, error) {
		return nil, tui.ErrAborted
	}

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "snippet")
	require.NoError(t, err, "aborting the picker is a clean no-op exit")
	require.Empty(t, child.calls)
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
	for _, flag := range []string{"-c", "-f", "-d", "-l", "-p", "-r", "-t"} {
		require.Contains(t, out, flag, "shared flag %s missing from help", flag)
	}
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

func TestRun_ProfileFlagSingleTarget(t *testing.T) {
	setTestProfiles(t, "rch-platform-dev-coffee", "rch-platform-prod-coffee")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	d := t.TempDir()
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)
	deps.resolveTargets = func(context.Context) ([]runner.Target, error) {
		return nil, errors.New("picker not expected when -p is given")
	}

	_, stderr, err := runRunCmd(t, deps, "run", "-d", d, "-c", "aws s3 ls", "-p", "dev-coffee", "-r", "us-west-2")
	require.NoError(t, err)
	require.Len(t, child.calls, 1)
	require.Equal(t, "rch-platform-dev-coffee", child.calls[0].env["AWS_PROFILE"], "-p does substring matching")
	require.Equal(t, "us-west-2", child.calls[0].env["AWS_REGION"])
	require.Contains(t, stderr, "matched", "the substring match is announced on stderr")
}

func TestRun_ProfileFlagAndFilterRejected(t *testing.T) {
	d := t.TempDir()
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "-c", "aws s3 ls", "-p", "dev", "prod")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not both")
	require.Empty(t, child.calls)
}

func TestRun_HeaderSuppliesProfileDefault(t *testing.T) {
	setTestProfiles(t, "prod")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	d := t.TempDir()
	writeFileT(t, d, "audit", "# profile: prod\n# region: eu-west-1\naws iam list-users\n", false)
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)
	deps.resolveTargets = func(context.Context) ([]runner.Target, error) {
		return nil, errors.New("picker not expected when the header names a profile")
	}

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "audit")
	require.NoError(t, err)
	require.Len(t, child.calls, 1)
	require.Equal(t, "prod", child.calls[0].env["AWS_PROFILE"])
	require.Equal(t, "eu-west-1", child.calls[0].env["AWS_REGION"])
	require.Equal(t, "aws iam list-users\n", child.calls[0].args[2], "header is stripped from the body")
}

func TestRun_TargetsFlagLeavesCommandPickable(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "vpc-cidrs", "aws ec2 describe-vpcs\n", false)
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)
	deps.isTerminal = func() bool { return true }
	deps.selectCommand = func(names []string) (string, error) {
		require.Equal(t, []string{"vpc-cidrs"}, names)
		return "vpc-cidrs", nil
	}
	deps.resolveTargets = func(context.Context) ([]runner.Target, error) {
		return nil, errors.New("target picker not expected when -t is given")
	}

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "-t", "qa:us-west-2 ops")
	require.NoError(t, err)
	require.Len(t, child.calls, 2, "-t names the targets while the command is picked")
	require.Equal(t, "qa", child.calls[0].env["AWS_PROFILE"])
	require.Equal(t, "us-west-2", child.calls[0].env["AWS_REGION"])
	require.Equal(t, "ops", child.calls[1].env["AWS_PROFILE"])
}

func TestRun_TargetsFlagAndPositionalFilterRejected(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "snippet", "echo hi\n", false)
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "-t", "dev", "snippet", "prod")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not both")
	require.Empty(t, child.calls)
}

func TestRun_TargetsFlagEquivalentToPositional(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "snippet", "echo hi\n", false)
	child := &childRecorder{}
	deps := newTestDeps(t, d, child)

	_, _, err := runRunCmd(t, deps, "run", "-d", d, "snippet", "-t", "qa:eu-west-1")
	require.NoError(t, err)
	require.Len(t, child.calls, 1)
	require.Equal(t, "qa", child.calls[0].env["AWS_PROFILE"])
	require.Equal(t, "eu-west-1", child.calls[0].env["AWS_REGION"])
}
