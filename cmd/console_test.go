package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kedwards/awst/v3/internal/console"
	"github.com/kedwards/awst/v3/internal/creds"
	"github.com/kedwards/awst/v3/internal/sso"
)

func consoleTestDeps(token string, c aws.Credentials, opened *string) consoleDeps {
	return consoleDeps{
		providerFactory: func(_ context.Context, _, region string) (creds.Provider, string, error) {
			return stubProvider{creds: c}, region, nil
		},
		signinToken: func(_ context.Context, _ console.Credentials) (string, error) {
			return token, nil
		},
		openBrowser: func(url string) error {
			if opened != nil {
				*opened = url
			}
			return nil
		},
		// Default: treat the profile as non-SSO so auto-login is skipped and
		// these tests focus on the federation path. Auto-login has its own test.
		sessionLoader: func(context.Context, string, string) (sso.SSOSession, error) {
			return sso.SSOSession{}, errors.New("not an sso profile")
		},
	}
}

func runConsole(t *testing.T, d consoleDeps, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newConsoleCmd(d))
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func tempCreds() aws.Credentials {
	return aws.Credentials{AccessKeyID: "AKIA", SecretAccessKey: "s", SessionToken: "t"}
}

func TestConsole_OpensBrowserWithLoginURL(t *testing.T) {
	var opened string
	d := consoleTestDeps("signin-xyz", tempCreds(), &opened)

	stdout, _, err := runConsole(t, d, "console", "dev", "-r", "us-east-1")
	require.NoError(t, err)
	require.Contains(t, opened, "Action=login")
	require.Contains(t, opened, "SigninToken=signin-xyz")
	require.Contains(t, opened, "console.aws.amazon.com")
	require.Empty(t, stdout, "opening a browser is quiet — nothing printed")
}

func TestConsole_ProfileFlagEquivalentToPositional(t *testing.T) {
	var opened string
	d := consoleTestDeps("signin-xyz", tempCreds(), &opened)

	_, _, err := runConsole(t, d, "console", "--profile", "dev", "-r", "us-east-1")
	require.NoError(t, err)
	require.Contains(t, opened, "SigninToken=signin-xyz")
}

func TestConsole_ProfileFlagAndPositionalConflict(t *testing.T) {
	d := consoleTestDeps("tok", tempCreds(), nil)

	_, _, err := runConsole(t, d, "console", "dev", "--profile", "dev")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not both")
}

func TestConsole_ServiceFlagTargetsServiceHome(t *testing.T) {
	var opened string
	d := consoleTestDeps("tok", tempCreds(), &opened)

	_, _, err := runConsole(t, d, "console", "dev", "-r", "eu-west-2", "-s", "ec2")
	require.NoError(t, err)
	require.Contains(t, opened, "eu-west-2.console.aws.amazon.com%2Fec2%2Fhome")
}

func TestConsole_NoBrowserPrintsButDoesNotOpen(t *testing.T) {
	var opened string
	d := consoleTestDeps("tok", tempCreds(), &opened)

	stdout, _, err := runConsole(t, d, "console", "dev", "--no-browser")
	require.NoError(t, err)
	require.Empty(t, opened, "--no-browser must not open the browser")
	require.Contains(t, stdout, "Action=login", "--no-browser prints the URL on stdout")
}

func TestConsole_RequiresSessionToken(t *testing.T) {
	var opened string
	// Long-term creds (no session token) can't be federated.
	d := consoleTestDeps("tok", aws.Credentials{AccessKeyID: "AKIA", SecretAccessKey: "s"}, &opened)

	_, _, err := runConsole(t, d, "console", "dev")
	require.Error(t, err)
	require.Contains(t, err.Error(), "awst login")
	require.Empty(t, opened)
}

func TestConsole_ContainerFlag_OpensFirefoxContainer(t *testing.T) {
	var browserURL, containerURL string
	d := consoleTestDeps("tok", tempCreds(), &browserURL)
	d.openContainer = func(u string) error { containerURL = u; return nil }

	_, _, err := runConsole(t, d, "console", "dev", "-r", "us-east-1", "--container")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(containerURL, "ext+granted-containers:"), "got %q", containerURL)
	require.Contains(t, containerURL, "name=dev")
	require.Contains(t, containerURL, "Action%3Dlogin", "wraps the escaped federation URL")
	require.Empty(t, browserURL, "container mode must not use the plain browser opener")
}

func TestConsole_ContainerViaEnv(t *testing.T) {
	t.Setenv("AWST_CONSOLE_CONTAINER", "1")
	var browserURL, containerURL string
	d := consoleTestDeps("tok", tempCreds(), &browserURL)
	d.openContainer = func(u string) error { containerURL = u; return nil }

	_, _, err := runConsole(t, d, "console", "dev")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(containerURL, "ext+granted-containers:"))
	require.Empty(t, browserURL)
}

func TestConsole_ContainerNoBrowser_PrintsButDoesNotOpen(t *testing.T) {
	var browserURL, containerURL string
	d := consoleTestDeps("tok", tempCreds(), &browserURL)
	d.openContainer = func(u string) error { containerURL = u; return nil }

	stdout, _, err := runConsole(t, d, "console", "dev", "--container", "--no-browser")
	require.NoError(t, err)
	require.Empty(t, containerURL, "--no-browser must not launch Firefox")
	require.Empty(t, browserURL)
	require.Contains(t, stdout, "ext+granted-containers:", "--no-browser prints the container URL on stdout")
}

func TestConsole_AutoLoginWhenNoCachedToken(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig) // defines profile "dev" -> sso_session "my-sso"
	d := consoleTestDeps("tok", tempCreds(), new(string))

	// Real SSO collaborators backed by stubs: empty cache forces the device flow.
	loginDep := loginTestDeps(t, cfg, nil)
	d.sessionLoader = loginDep.sessionLoader
	d.oidcFactory = loginDep.oidcFactory
	d.cache = loginDep.cache
	d.sleep = loginDep.sleep
	d.now = loginDep.now

	stdout, stderr, err := runConsole(t, d, "console", "dev", "--no-browser")
	require.NoError(t, err)
	// Device flow ran (prompt shown) and a token was cached for the session.
	require.Contains(t, stderr, "example.aws/device", "auto-login should run the device flow")
	_, statErr := os.Stat(d.cache.Path("my-sso"))
	require.NoError(t, statErr, "auto-login should cache a token")
	// And it still produced the console URL afterwards (on stdout, --no-browser).
	require.Contains(t, stdout, "Action=login")
}

func TestConsole_HelpFlag(t *testing.T) {
	d := consoleTestDeps("tok", tempCreds(), nil)
	out, _, err := runConsole(t, d, "console", "-h")
	require.NoError(t, err)
	require.Contains(t, out, "console [profile]")
	require.True(t, strings.Contains(out, "--service") && strings.Contains(out, "--no-browser"))
}
