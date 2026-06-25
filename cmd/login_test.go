package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kedwards/awst/v3/internal/creds"
	"github.com/kedwards/awst/v3/internal/sso"
	"github.com/kedwards/awst/v3/internal/tui"
)

type stubOIDC struct {
	regOut  *ssooidc.RegisterClientOutput
	devOut  *ssooidc.StartDeviceAuthorizationOutput
	tokOuts []*ssooidc.CreateTokenOutput
	tokN    int
}

func (s *stubOIDC) RegisterClient(_ context.Context, _ *ssooidc.RegisterClientInput, _ ...func(*ssooidc.Options)) (*ssooidc.RegisterClientOutput, error) {
	return s.regOut, nil
}
func (s *stubOIDC) StartDeviceAuthorization(_ context.Context, _ *ssooidc.StartDeviceAuthorizationInput, _ ...func(*ssooidc.Options)) (*ssooidc.StartDeviceAuthorizationOutput, error) {
	return s.devOut, nil
}
func (s *stubOIDC) CreateToken(_ context.Context, _ *ssooidc.CreateTokenInput, _ ...func(*ssooidc.Options)) (*ssooidc.CreateTokenOutput, error) {
	i := s.tokN
	s.tokN++
	if i >= len(s.tokOuts) {
		return nil, errors.New("stub exhausted")
	}
	return s.tokOuts[i], nil
}

func writeAWSConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func loginTestDeps(t *testing.T, configFile string, openBrowserCalled *bool) loginDeps {
	t.Helper()
	cacheDir := filepath.Join(t.TempDir(), "cache")
	return loginDeps{
		sessionLoader: func(ctx context.Context, profile, _ string) (sso.SSOSession, error) {
			return sso.LoadSSOSession(ctx, profile, configFile)
		},
		oidcFactory: func(ctx context.Context, region string) (sso.OIDCClient, error) {
			return &stubOIDC{
				regOut: &ssooidc.RegisterClientOutput{
					ClientId:     aws.String("cid"),
					ClientSecret: aws.String("csec"),
				},
				devOut: &ssooidc.StartDeviceAuthorizationOutput{
					DeviceCode:              aws.String("dev-code"),
					UserCode:                aws.String("ABCD-EFGH"),
					VerificationUriComplete: aws.String("https://example.aws/device?user_code=ABCD-EFGH"),
					Interval:                1,
					ExpiresIn:               600,
				},
				tokOuts: []*ssooidc.CreateTokenOutput{
					{AccessToken: aws.String("atk"), RefreshToken: aws.String("rt"), ExpiresIn: 3600},
				},
			}, nil
		},
		cache: sso.NewCache(cacheDir),
		openBrowser: func(url string) error {
			if openBrowserCalled != nil {
				*openBrowserCalled = true
			}
			return nil
		},
		sleep: func(time.Duration) {},
		now:   func() time.Time { return time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC) },
		listProfiles: func() ([]string, error) {
			return readProfileNamesFromFile(t, configFile), nil
		},
		selectProfile: func([]tui.ProfileItem) (string, error) {
			t.Fatal("selectProfile should not be called")
			return "", nil
		},
		isTerminal: func() bool { return true },
		providerFactory: func(_ context.Context, _, _ string) (creds.Provider, string, error) {
			return stubProvider{creds: aws.Credentials{
				AccessKeyID:     "AKIA-TEST",
				SecretAccessKey: "secret",
				SessionToken:    "token",
			}}, "us-east-1", nil
		},
	}
}

// readProfileNamesFromFile returns the [profile X]/[default] names in an AWS
// config file — a tiny test helper mirroring defaultListProfiles, but pointed
// at an arbitrary path.
func readProfileNamesFromFile(t *testing.T, path string) []string {
	t.Helper()
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
			continue
		}
		body := strings.TrimSpace(line[1 : len(line)-1])
		switch {
		case body == "default":
			out = append(out, "default")
		case strings.HasPrefix(body, "profile "):
			out = append(out, strings.TrimSpace(strings.TrimPrefix(body, "profile ")))
		}
	}
	return out
}

const ssoSessionConfig = `
[profile dev]
sso_session = my-sso
sso_account_id = 123456789012
sso_role_name = Developer
region = us-east-1

[sso-session my-sso]
sso_start_url = https://my-org.awsapps.com/start
sso_region = us-east-1
sso_registration_scopes = sso:account:access
`

const legacyConfig = `
[profile legacy]
sso_start_url = https://legacy.awsapps.com/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Developer
`

func runLogin(t *testing.T, d loginDeps, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newLoginCmd(d))

	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func TestLogin_HappyPath_CachesTokenAndPrintsPrompt(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	var browserOpened bool
	d := loginTestDeps(t, cfg, &browserOpened)

	_, stderr, err := runLogin(t, d, "login", "dev")

	require.NoError(t, err)
	require.Contains(t, stderr, "https://example.aws/device?user_code=ABCD-EFGH")
	require.Contains(t, stderr, "ABCD-EFGH")
	require.True(t, browserOpened, "browser open helper should be invoked by default")

	// Token should be in the cache at the SDK-readable path.
	tokPath := d.cache.Path("my-sso")
	_, err = os.Stat(tokPath)
	require.NoError(t, err, "expected token cache at %s", tokPath)
}

func TestLogin_ValidCachedTokenSkipsSSO(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	var browserOpened bool
	d := loginTestDeps(t, cfg, &browserOpened)

	// Pre-seed a token that is still valid relative to the stubbed clock.
	require.NoError(t, d.cache.Save("my-sso", sso.Token{
		AccessToken: "cached",
		ExpiresAt:   d.now().Add(time.Hour),
	}))

	_, stderr, err := runLogin(t, d, "login", "dev")

	require.NoError(t, err)
	require.NotContains(t, stderr, "example.aws/device", "device flow must not run")
	require.False(t, browserOpened, "browser must not open when token is valid")
}

func TestLogin_NoExport_SilentOnSuccess(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	d := loginTestDeps(t, cfg, nil)
	require.NoError(t, d.cache.Save("my-sso", sso.Token{
		AccessToken: "cached",
		ExpiresAt:   d.now().Add(time.Hour),
	}))

	stdout, stderr, err := runLogin(t, d, "login", "dev")
	require.NoError(t, err)
	require.Empty(t, stdout, "no --export: nothing on stdout")
	require.NotContains(t, stderr, "Logged in")
	require.NotContains(t, stderr, "Already logged in")
}

func TestLogin_ExpiredCachedTokenRelogs(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	var browserOpened bool
	d := loginTestDeps(t, cfg, &browserOpened)

	// Pre-seed an already-expired token; login should run the device flow.
	require.NoError(t, d.cache.Save("my-sso", sso.Token{
		AccessToken: "stale",
		ExpiresAt:   d.now().Add(-time.Hour),
	}))

	_, stderr, err := runLogin(t, d, "login", "dev")

	require.NoError(t, err)
	require.Contains(t, stderr, "example.aws/device", "expired token should trigger device flow")
	require.True(t, browserOpened)
}

func TestLogin_ExportPrintsCredsToStdout(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	d := loginTestDeps(t, cfg, nil)

	stdout, stderr, err := runLogin(t, d, "login", "dev", "--export", "--no-browser")
	require.NoError(t, err)

	// Exports go to stdout (so eval "$(...)" captures only these)...
	require.Contains(t, stdout, `export AWS_PROFILE="dev"`)
	require.Contains(t, stdout, `export AWS_ACCESS_KEY_ID="AKIA-TEST"`)
	require.Contains(t, stdout, `export AWS_SESSION_TOKEN="token"`)
	require.Contains(t, stdout, `export AWS_REGION="us-east-1"`)
	require.Contains(t, stdout, `export AWS_DEFAULT_REGION="us-east-1"`)
	// ...while status text stays on stderr.
	require.NotContains(t, stdout, "Logged in")
	require.Contains(t, stderr, "Logged in")
}

func TestLogin_ExportRegionFlagPassedThrough(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	d := loginTestDeps(t, cfg, nil)
	var gotRegion string
	d.providerFactory = func(_ context.Context, _, region string) (creds.Provider, string, error) {
		gotRegion = region
		return stubProvider{creds: aws.Credentials{AccessKeyID: "AKIA", SecretAccessKey: "s", SessionToken: "t"}}, region, nil
	}

	stdout, _, err := runLogin(t, d, "login", "dev", "--export", "--no-browser", "-r", "eu-west-2")
	require.NoError(t, err)
	require.Equal(t, "eu-west-2", gotRegion, "--region must reach the provider factory")
	require.Contains(t, stdout, `export AWS_REGION="eu-west-2"`)
	require.Contains(t, stdout, `export AWS_DEFAULT_REGION="eu-west-2"`)
}

func TestLogin_ExportRegionDefaultsToUsEast1(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	d := loginTestDeps(t, cfg, nil)
	// Neither a flag nor a resolved region — should fall back to us-east-1.
	d.providerFactory = func(_ context.Context, _, _ string) (creds.Provider, string, error) {
		return stubProvider{creds: aws.Credentials{AccessKeyID: "AKIA", SecretAccessKey: "s", SessionToken: "t"}}, "", nil
	}

	stdout, _, err := runLogin(t, d, "login", "dev", "--export", "--no-browser")
	require.NoError(t, err)
	require.Contains(t, stdout, `export AWS_REGION="us-east-1"`)
	require.Contains(t, stdout, `export AWS_DEFAULT_REGION="us-east-1"`)
}

func TestLogin_NoBrowserFlagSuppressesOpen(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	var browserOpened bool
	d := loginTestDeps(t, cfg, &browserOpened)

	_, _, err := runLogin(t, d, "login", "dev", "--no-browser")

	require.NoError(t, err)
	require.False(t, browserOpened, "--no-browser should suppress browser open")
}

func TestLogin_LegacyProfileRejected(t *testing.T) {
	cfg := writeAWSConfig(t, legacyConfig)
	d := loginTestDeps(t, cfg, nil)

	_, _, err := runLogin(t, d, "login", "legacy")

	require.Error(t, err)
	require.Contains(t, err.Error(), "sso_session")
}

func TestLogin_ProfileFlagEquivalentToPositional(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	d := loginTestDeps(t, cfg, nil)

	_, stderr, err := runLogin(t, d, "login", "--profile", "dev")
	require.NoError(t, err)
	require.Contains(t, stderr, "ABCD-EFGH")
	_, err = os.Stat(d.cache.Path("my-sso"))
	require.NoError(t, err, "flag form should cache the token just like positional")
}

func TestLogin_ProfileFlagAndPositionalConflict(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig)
	d := loginTestDeps(t, cfg, nil)

	_, _, err := runLogin(t, d, "login", "dev", "--profile", "dev")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not both")
}

func TestLogin_HelpFlag(t *testing.T) {
	d := loginTestDeps(t, "", nil)
	out, _, err := runLogin(t, d, "login", "-h")
	require.NoError(t, err)
	require.Contains(t, out, "login [profile]")
	require.Contains(t, out, "--no-browser")
}

// multiProfileConfig has two SSO-capable profiles (dev, prod, both via my-sso)
// plus a non-SSO profile (plain) that the picker must filter out.
const multiProfileConfig = ssoSessionConfig + `
[profile prod]
sso_session = my-sso
sso_account_id = 999999999999
sso_role_name = Admin
region = us-east-1

[profile plain]
region = us-west-2
`

func TestLogin_NoArg_PickerListsOnlySSOProfiles(t *testing.T) {
	cfg := writeAWSConfig(t, multiProfileConfig)
	d := loginTestDeps(t, cfg, nil)

	var offered []string
	d.selectProfile = func(items []tui.ProfileItem) (string, error) {
		for _, it := range items {
			offered = append(offered, it.Profile)
			require.Equal(t, "my-sso", it.Session)
		}
		return "dev", nil
	}

	_, _, err := runLogin(t, d, "login")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"dev", "prod"}, offered, "non-SSO 'plain' must be filtered out")
}

func TestLogin_NoArg_SingleProfileAutoSelected(t *testing.T) {
	cfg := writeAWSConfig(t, ssoSessionConfig) // only "dev" is SSO-capable
	d := loginTestDeps(t, cfg, nil)
	// selectProfile stays as the t.Fatal stub — it must not be called.

	_, stderr, err := runLogin(t, d, "login")
	require.NoError(t, err)
	require.Contains(t, stderr, "Using the only SSO profile: dev")
}

func TestLogin_NoArg_NoSSOProfiles(t *testing.T) {
	cfg := writeAWSConfig(t, legacyConfig) // legacy profile has no sso_session
	d := loginTestDeps(t, cfg, nil)

	_, _, err := runLogin(t, d, "login")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no SSO-capable profiles")
}

func TestLogin_NoArg_Aborted(t *testing.T) {
	cfg := writeAWSConfig(t, multiProfileConfig)
	d := loginTestDeps(t, cfg, nil)
	d.selectProfile = func([]tui.ProfileItem) (string, error) {
		return "", tui.ErrAborted
	}

	_, _, err := runLogin(t, d, "login")
	require.NoError(t, err, "aborting the picker is a clean no-op")
}

func TestLogin_NoArg_NotATerminal(t *testing.T) {
	cfg := writeAWSConfig(t, multiProfileConfig)
	d := loginTestDeps(t, cfg, nil)
	d.isTerminal = func() bool { return false }

	_, _, err := runLogin(t, d, "login")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a terminal")
}
