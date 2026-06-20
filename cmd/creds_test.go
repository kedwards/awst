package cmd

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kedwards/aws-tools/internal/creds"
)

func runCmd(t *testing.T, d credsDeps, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newCredsCmd(d))

	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func testDeps(t *testing.T) credsDeps {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "creds")
	return credsDeps{
		store: creds.NewStore(dir),
		providerFactory: func(ctx context.Context, profile, region string) (creds.Provider, string, error) {
			return stubProvider{
				creds: aws.Credentials{
					AccessKeyID:     "AKIA-from-stub",
					SecretAccessKey: "secret",
					SessionToken:    "token",
				},
			}, "us-east-1", nil
		},
		now: func() time.Time { return time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC) },
	}
}

type stubProvider struct {
	creds aws.Credentials
	err   error
}

func (s stubProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	return s.creds, s.err
}

func TestCredsList_NoCredsDir(t *testing.T) {
	d := testDeps(t)
	out, _, err := runCmd(t, d, "creds", "list")
	require.NoError(t, err)
	require.Equal(t, "No stored credentials found\n", out)
}

func TestCredsStore_HelpFlag(t *testing.T) {
	d := testDeps(t)
	out, _, err := runCmd(t, d, "creds", "store", "-h")
	require.NoError(t, err)
	require.Contains(t, out, "Usage:")
	require.Contains(t, out, "creds store <profile>")
}

func TestCredsUse_HelpFlag(t *testing.T) {
	d := testDeps(t)
	out, _, err := runCmd(t, d, "creds", "use", "-h")
	require.NoError(t, err)
	require.Contains(t, out, "Usage:")
	require.Contains(t, out, "creds use <profile>")
}

func TestCredsClear_UnknownProfile(t *testing.T) {
	d := testDeps(t)
	_, _, err := runCmd(t, d, "creds", "clear", "nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), `"nope"`)
}

func TestCredsStore_WritesAndPrintsExports(t *testing.T) {
	d := testDeps(t)
	out, _, err := runCmd(t, d, "creds", "store", "dev")
	require.NoError(t, err)
	require.Contains(t, out, `export AWS_ACCESS_KEY_ID="AKIA-from-stub"`)
	require.Contains(t, out, `export AWS_REGION="us-east-1"`)
	require.Contains(t, out, `export AWS_PROFILE="dev"`)

	// Round-trip: stored file should be loadable.
	loaded, err := d.store.Load("dev")
	require.NoError(t, err)
	require.Equal(t, "AKIA-from-stub", loaded.AccessKeyID)
	require.Equal(t, "us-east-1", loaded.Region)
}

func TestCredsStore_PowerShellShell(t *testing.T) {
	d := testDeps(t)
	out, _, err := runCmd(t, d, "creds", "store", "dev", "--shell", "powershell")
	require.NoError(t, err)
	require.Contains(t, out, `$env:AWS_ACCESS_KEY_ID = 'AKIA-from-stub'`)
	require.Contains(t, out, `$env:AWS_PROFILE = 'dev'`)
	require.NotContains(t, out, "export ")
}

func TestCredsUse_InvalidShell(t *testing.T) {
	d := testDeps(t)
	require.NoError(t, d.store.Save("dev", creds.Credentials{AccessKeyID: "a", SecretAccessKey: "s", SessionToken: "t"}))
	_, _, err := runCmd(t, d, "creds", "use", "dev", "--shell", "fish")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown shell")
}

func TestCredsStore_ProviderError(t *testing.T) {
	sentinel := errors.New("SSO token expired")
	d := testDeps(t)
	d.providerFactory = func(ctx context.Context, profile, region string) (creds.Provider, string, error) {
		return stubProvider{err: sentinel}, "", nil
	}

	_, _, err := runCmd(t, d, "creds", "store", "dev")
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
}

func TestCredsUse_MissingProfile(t *testing.T) {
	d := testDeps(t)
	_, _, err := runCmd(t, d, "creds", "use", "nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no stored credentials")
}

func TestCredsUse_RoundTrip(t *testing.T) {
	d := testDeps(t)
	require.NoError(t, d.store.Save("dev", creds.Credentials{
		AccessKeyID: "stored-AKIA", SecretAccessKey: "s", SessionToken: "t",
	}))

	out, _, err := runCmd(t, d, "creds", "use", "dev")
	require.NoError(t, err)
	require.Contains(t, out, `export AWS_ACCESS_KEY_ID="stored-AKIA"`)
}

func TestCredsList_WithProfiles(t *testing.T) {
	d := testDeps(t)
	require.NoError(t, d.store.Save("dev", creds.Credentials{AccessKeyID: "a", SecretAccessKey: "b", SessionToken: "c"}))

	out, _, err := runCmd(t, d, "creds", "list")
	require.NoError(t, err)
	require.Contains(t, out, "dev")
	require.Contains(t, out, "ago")
}
