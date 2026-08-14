package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

// runShellSplit keeps stdout and stderr apart, which install needs: status
// text goes to stderr so --print stdout stays clean.
func runShellSplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newShellCmd())

	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
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

func TestShellInit_Fish(t *testing.T) {
	out, err := runShell(t, "shell", "init", "--fish")
	require.NoError(t, err)
	require.Contains(t, out, "function awst")
	require.Contains(t, out, "command awst login --export --shell fish $argv | source")
	require.Contains(t, out, "command awst logout --export --shell fish $argv | source")
	require.Contains(t, out, "set -gx "+shellInitEnvVar+" '"+version+"'")
}

// The marker is what lets login tell a shell with the wrapper loaded from one
// without it, so it has to be in the emitted script and carry the version.
func TestShellInit_EmitsVersionedMarker(t *testing.T) {
	out, err := runShell(t, "shell", "init")
	require.NoError(t, err)
	require.Contains(t, out, "export "+shellInitEnvVar+"='"+version+"'")

	psOut, err := runShell(t, "shell", "init", "--powershell")
	require.NoError(t, err)
	require.Contains(t, psOut, "$env:"+shellInitEnvVar+" = '"+version+"'")

	fishOut, err := runShell(t, "shell", "init", "--fish")
	require.NoError(t, err)
	require.Contains(t, fishOut, "set -gx "+shellInitEnvVar+" '"+version+"'")
}

func TestShellInit_MarkerRoundTripsToDetection(t *testing.T) {
	out, err := runShell(t, "shell", "init")
	require.NoError(t, err)
	require.Contains(t, out, shellInitEnvVar)

	si := detectShellIntegration(func(k string) string {
		if k == shellInitEnvVar {
			return version
		}
		return ""
	})
	require.True(t, si.loaded)
	require.False(t, si.stale())

	stale := detectShellIntegration(func(string) string { return "0.0.1-old" })
	require.True(t, stale.loaded)
	require.True(t, stale.stale())

	absent := detectShellIntegration(func(string) string { return "" })
	require.False(t, absent.loaded)
	require.False(t, absent.stale(), "an absent wrapper is not a stale one")
}

func TestShellInstall_Print_WritesNoFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")

	stdout, _, err := runShellSplit(t, "shell", "install", "--print")
	require.NoError(t, err)
	require.Contains(t, stdout, `eval "$(awst shell init)"`)
	require.Contains(t, stdout, shellInstallBegin)

	_, statErr := os.Stat(filepath.Join(home, ".bashrc"))
	require.True(t, os.IsNotExist(statErr), "--print must not touch the rc file")
}

func TestShellInstall_CreatesBashrc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")

	_, stderr, err := runShellSplit(t, "shell", "install")
	require.NoError(t, err)

	rc := filepath.Join(home, ".bashrc")
	require.Contains(t, stderr, rc)
	require.Contains(t, stderr, "source "+rc)

	b, readErr := os.ReadFile(rc)
	require.NoError(t, readErr)
	require.Contains(t, string(b), `eval "$(awst shell init)"`)
	require.Contains(t, string(b), shellInstallBegin)
	require.Contains(t, string(b), shellInstallEnd)
}

func TestShellInstall_ZshFromShellEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/usr/bin/zsh")

	_, _, err := runShellSplit(t, "shell", "install")
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(home, ".zshrc"))
	require.NoError(t, statErr, "zsh should get .zshrc")
	_, bashErr := os.Stat(filepath.Join(home, ".bashrc"))
	require.True(t, os.IsNotExist(bashErr), "bash rc should be left alone")
}

func TestShellInstall_PreservesExistingContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	rc := filepath.Join(home, ".bashrc")
	require.NoError(t, os.WriteFile(rc, []byte("export EDITOR=vim"), 0o644))

	_, _, err := runShellSplit(t, "shell", "install")
	require.NoError(t, err)

	b, readErr := os.ReadFile(rc)
	require.NoError(t, readErr)
	require.Contains(t, string(b), "export EDITOR=vim")
	require.Contains(t, string(b), `eval "$(awst shell init)"`)
	// A file with no trailing newline must not run into the block.
	require.NotContains(t, string(b), "export EDITOR=vim#")
}

func TestShellInstall_IsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	rc := filepath.Join(home, ".bashrc")

	_, _, err := runShellSplit(t, "shell", "install")
	require.NoError(t, err)
	first, err := os.ReadFile(rc)
	require.NoError(t, err)

	_, stderr, err := runShellSplit(t, "shell", "install")
	require.NoError(t, err)
	require.Contains(t, stderr, "already installed")

	second, err := os.ReadFile(rc)
	require.NoError(t, err)
	require.Equal(t, string(first), string(second), "re-running must not change the file")
	require.Equal(t, 1, strings.Count(string(second), shellInstallBegin))
}

func TestShellInstall_ForceRewritesOneBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	rc := filepath.Join(home, ".bashrc")

	_, _, err := runShellSplit(t, "shell", "install")
	require.NoError(t, err)
	_, _, err = runShellSplit(t, "shell", "install", "--force")
	require.NoError(t, err)

	b, readErr := os.ReadFile(rc)
	require.NoError(t, readErr)
	require.Equal(t, 1, strings.Count(string(b), shellInstallBegin), "--force replaces the block instead of stacking")
	require.Equal(t, 1, strings.Count(string(b), `eval "$(awst shell init)"`))
}

// A line the user wrote themselves is theirs; don't silently duplicate it.
func TestShellInstall_LeavesHandWrittenLineAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	rc := filepath.Join(home, ".bashrc")
	require.NoError(t, os.WriteFile(rc, []byte("eval \"$(awst shell init)\"\n"), 0o644))

	_, stderr, err := runShellSplit(t, "shell", "install")
	require.NoError(t, err)
	require.Contains(t, stderr, "did not write")

	b, readErr := os.ReadFile(rc)
	require.NoError(t, readErr)
	require.NotContains(t, string(b), shellInstallBegin)
	require.Equal(t, 1, strings.Count(string(b), "awst shell init"))
}

func TestShellInstall_FileFlagOverridesDetection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	target := filepath.Join(t.TempDir(), "config.fish")

	_, _, err := runShellSplit(t, "shell", "install", "--file", target)
	require.NoError(t, err)

	b, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	require.Contains(t, string(b), `eval "$(awst shell init)"`)
	_, statErr := os.Stat(filepath.Join(home, ".bashrc"))
	require.True(t, os.IsNotExist(statErr), "--file must win over $SHELL")
}

func TestShellInstall_FishFromShellEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/usr/bin/fish")

	_, stderr, err := runShellSplit(t, "shell", "install")
	require.NoError(t, err)

	rc := filepath.Join(home, ".config", "fish", "config.fish")
	require.Contains(t, stderr, rc)
	require.Contains(t, stderr, "source "+rc)

	b, readErr := os.ReadFile(rc)
	require.NoError(t, readErr)
	require.Contains(t, string(b), "awst shell init --fish | source")
}

func TestShellInstall_FishFlagWithFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.fish")

	_, stderr, err := runShellSplit(t, "shell", "install", "--fish", "--file", target)
	require.NoError(t, err)
	require.Contains(t, stderr, "source "+target)

	b, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	require.Contains(t, string(b), "awst shell init --fish | source")
}

func TestShellInstall_FishFlagUsesFishDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")

	_, stderr, err := runShellSplit(t, "shell", "install", "--fish")
	require.NoError(t, err)

	rc := filepath.Join(home, ".config", "fish", "config.fish")
	require.Contains(t, stderr, rc)
	b, readErr := os.ReadFile(rc)
	require.NoError(t, readErr)
	require.Contains(t, string(b), "awst shell init --fish | source")
}

func TestShellInstall_PowerShellNeedsFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/bash")

	_, _, err := runShellSplit(t, "shell", "install", "--powershell")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--file")
}

func TestShellInstall_PowerShellBlockWithFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "profile.ps1")

	_, stderr, err := runShellSplit(t, "shell", "install", "--powershell", "--file", target)
	require.NoError(t, err)
	require.Contains(t, stderr, ". "+target, "PowerShell reloads with dot-sourcing, not source")

	b, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	require.Contains(t, string(b), "awst shell init --powershell | Out-String | Invoke-Expression")
}
func TestShellInstall_ForceFailsOnMalformedManagedBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	rc := filepath.Join(home, ".bashrc")
	original := shellInstallBegin + "\n" + `eval "$(awst shell init)"` + "\n" + "export EDITOR=vim\n"
	require.NoError(t, os.WriteFile(rc, []byte(original), 0o644))

	_, _, err := runShellSplit(t, "shell", "install", "--force")
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing its closing marker")

	b, readErr := os.ReadFile(rc)
	require.NoError(t, readErr)
	require.Equal(t, original, string(b), "malformed managed block must be left untouched on failure")
}

func TestShellInit_RejectsMultipleShellFlags(t *testing.T) {
	_, err := runShell(t, "shell", "init", "--fish", "--powershell")
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most one")
}

func TestShellInstall_RejectsMultipleShellFlags(t *testing.T) {
	_, _, err := runShellSplit(t, "shell", "install", "--fish", "--powershell")
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most one")
}

func TestShellInstall_UnknownShellIsAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/usr/bin/elvish")

	_, _, err := runShellSplit(t, "shell", "install")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--file")
}

func TestShellInstall_UnsetShellIsAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "")

	_, _, err := runShellSplit(t, "shell", "install")
	require.Error(t, err)
	require.Contains(t, err.Error(), "$SHELL is not set")
}
