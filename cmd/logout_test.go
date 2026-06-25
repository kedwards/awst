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

func runLogout(t *testing.T, d logoutDeps, args ...string) (stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newLogoutCmd(d))
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return errBuf.String(), err
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

	stderr, err := runLogout(t, d, "logout", "dev")
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

	stderr, err := runLogout(t, d, "logout", "--profile", "dev")
	require.NoError(t, err)
	require.Contains(t, stderr, "my-sso")
	_, statErr := os.Stat(cache.Path("my-sso"))
	require.True(t, os.IsNotExist(statErr), "flag form should clear the session just like positional")
}

func TestLogout_ProfileFlagAndPositionalConflict(t *testing.T) {
	d := logoutDeps{cache: sso.NewCache(t.TempDir())}
	_, err := runLogout(t, d, "logout", "dev", "--profile", "dev")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not both")
}

func TestLogout_NoArg_ClearsAll(t *testing.T) {
	cache := sso.NewCache(t.TempDir())
	require.NoError(t, cache.Save("s1", sso.Token{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)}))
	require.NoError(t, cache.Save("s2", sso.Token{AccessToken: "b", ExpiresAt: time.Now().Add(time.Hour)}))

	d := logoutDeps{cache: cache}
	stderr, err := runLogout(t, d, "logout")
	require.NoError(t, err)
	require.Contains(t, stderr, "Cleared 2")
	_, e1 := os.Stat(cache.Path("s1"))
	_, e2 := os.Stat(cache.Path("s2"))
	require.True(t, os.IsNotExist(e1) && os.IsNotExist(e2))
}

func TestLogout_NoArg_EmptyCacheIsClean(t *testing.T) {
	d := logoutDeps{cache: sso.NewCache(t.TempDir())}
	stderr, err := runLogout(t, d, "logout")
	require.NoError(t, err)
	require.Contains(t, stderr, "Cleared 0")
}
