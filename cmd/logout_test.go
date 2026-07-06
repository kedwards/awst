package cmd

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kedwards/awst/v3/internal/sso"
)

func runLogout(t *testing.T, d logoutDeps, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newLogoutCmd(d))
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func TestLogout_Profile_ClearsThatSession(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig) // profile "dev" -> sso_session "my-sso"
	cache := sso.NewCache(t.TempDir())
	require.NoError(t, cache.Save("my-sso", sso.Token{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)}))

	d := logoutDeps{
		sessionLoader: func(ctx context.Context, profile, _ string) (sso.SSOSession, error) {
			return sso.LoadSSOSession(ctx, profile, cfg)
		},
		cache: cache,
	}

	_, stderr, err := runLogout(t, d, "logout", "--clear-cache", "dev")
	require.NoError(t, err)
	require.Contains(t, stderr, "my-sso")
	_, statErr := os.Stat(cache.Path("my-sso"))
	require.True(t, os.IsNotExist(statErr), "token file should be gone")
}

func TestLogout_ProfileFlag_ClearsThatSession(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	cache := sso.NewCache(t.TempDir())
	require.NoError(t, cache.Save("my-sso", sso.Token{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)}))

	d := logoutDeps{
		sessionLoader: func(ctx context.Context, profile, _ string) (sso.SSOSession, error) {
			return sso.LoadSSOSession(ctx, profile, cfg)
		},
		cache: cache,
	}

	_, stderr, err := runLogout(t, d, "logout", "--clear-cache", "--profile", "dev")
	require.NoError(t, err)
	require.Contains(t, stderr, "my-sso")
	_, statErr := os.Stat(cache.Path("my-sso"))
	require.True(t, os.IsNotExist(statErr), "flag form should clear the session just like positional")
}

func TestLogout_ProfileFlagAndPositionalConflict(t *testing.T) {
	d := logoutDeps{cache: sso.NewCache(t.TempDir())}
	_, _, err := runLogout(t, d, "logout", "dev", "--profile", "dev")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not both")
}

func TestLogout_ClearCache_NoArg_ClearsAll(t *testing.T) {
	cache := sso.NewCache(t.TempDir())
	require.NoError(t, cache.Save("s1", sso.Token{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)}))
	require.NoError(t, cache.Save("s2", sso.Token{AccessToken: "b", ExpiresAt: time.Now().Add(time.Hour)}))

	d := logoutDeps{cache: cache}
	_, stderr, err := runLogout(t, d, "logout", "--clear-cache")
	require.NoError(t, err)
	require.Contains(t, stderr, "Cleared 2")
	_, e1 := os.Stat(cache.Path("s1"))
	_, e2 := os.Stat(cache.Path("s2"))
	require.True(t, os.IsNotExist(e1) && os.IsNotExist(e2))
}

func TestLogout_ClearCache_EmptyCacheIsClean(t *testing.T) {
	d := logoutDeps{cache: sso.NewCache(t.TempDir())}
	_, stderr, err := runLogout(t, d, "logout", "--clear-cache")
	require.NoError(t, err)
	require.Contains(t, stderr, "Cleared 0")
}

func TestLogout_Export_PrintsUnsetToStdout(t *testing.T) {
	d := logoutDeps{cache: sso.NewCache(t.TempDir())}
	stdout, stderr, err := runLogout(t, d, "logout", "--export")
	require.NoError(t, err)
	// Unset statements go to stdout (so eval "$(...)" clears the shell)...
	require.Contains(t, stdout, "unset ")
	require.Contains(t, stdout, "AWS_PROFILE")
	require.Contains(t, stdout, "AWS_ACCESS_KEY_ID")
	require.Contains(t, stdout, "AWS_REGION")
	// ...while status text stays on stderr.
	require.Contains(t, stderr, "Cleared AWS credential env vars")
	require.NotContains(t, stdout, "Cleared")
}

func TestLogout_Export_PowerShell(t *testing.T) {
	d := logoutDeps{cache: sso.NewCache(t.TempDir())}
	stdout, _, err := runLogout(t, d, "logout", "--export", "--shell", "powershell")
	require.NoError(t, err)
	require.Contains(t, stdout, "Remove-Item Env:AWS_PROFILE")
}

// The flipped default: `awst logout` (via the wrapper's --export) clears the
// shell env vars but leaves the cached SSO token untouched.
func TestLogout_Default_ClearsEnvButKeepsCache(t *testing.T) {
	cache := sso.NewCache(t.TempDir())
	require.NoError(t, cache.Save("my-sso", sso.Token{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)}))
	d := logoutDeps{cache: cache}

	stdout, _, err := runLogout(t, d, "logout", "--export")
	require.NoError(t, err)
	require.Contains(t, stdout, "unset ")
	_, statErr := os.Stat(cache.Path("my-sso"))
	require.NoError(t, statErr, "SSO token must survive a default logout")
}

// --clear-cache both clears the shell env and forgets the cached token.
func TestLogout_ClearCache_ClearsEnvAndCache(t *testing.T) {
	cache := sso.NewCache(t.TempDir())
	require.NoError(t, cache.Save("s1", sso.Token{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)}))
	d := logoutDeps{cache: cache}

	stdout, stderr, err := runLogout(t, d, "logout", "--clear-cache", "--export")
	require.NoError(t, err)
	require.Contains(t, stdout, "unset ")
	require.Contains(t, stderr, "Cleared 1")
	_, statErr := os.Stat(cache.Path("s1"))
	require.True(t, os.IsNotExist(statErr), "token must be gone with --clear-cache")
}

func TestLogout_NoExport_SilentStdout(t *testing.T) {
	d := logoutDeps{cache: sso.NewCache(t.TempDir())}
	stdout, _, err := runLogout(t, d, "logout")
	require.NoError(t, err)
	require.Empty(t, stdout, "no --export: nothing on stdout to eval")
}
