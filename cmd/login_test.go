package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kedwards/aws-tools/internal/sso"
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
	}
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

func TestLogin_HelpFlag(t *testing.T) {
	d := loginTestDeps(t, "", nil)
	out, _, err := runLogin(t, d, "login", "-h")
	require.NoError(t, err)
	require.Contains(t, out, "login <profile>")
	require.Contains(t, out, "--no-browser")
}

func TestLogin_MissingProfileArg(t *testing.T) {
	d := loginTestDeps(t, "", nil)
	_, _, err := runLogin(t, d, "login")
	require.Error(t, err)
}
