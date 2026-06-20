package sso

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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

const legacyOnlyConfig = `
[profile legacy]
sso_start_url = https://legacy.awsapps.com/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Developer
`

const malformedSessionConfig = `
[profile dev]
sso_session = my-sso
sso_account_id = 123456789012
sso_role_name = Developer
region = us-east-1

[sso-session my-sso]
region = us-east-1
sso_start_url = https://my-org.awsapps.com/start
`

const noSSOConfig = `
[profile plain]
region = us-east-1
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestLoadSSOSession_ReturnsSessionFields(t *testing.T) {
	cfg := writeConfig(t, ssoSessionConfig)

	got, err := LoadSSOSession(context.Background(), "dev", cfg)

	require.NoError(t, err)
	require.Equal(t, SSOSession{
		Name:     "my-sso",
		Region:   "us-east-1",
		StartURL: "https://my-org.awsapps.com/start",
	}, got)
}

func TestLoadSSOSession_ProfileNotFound_ListsAvailable(t *testing.T) {
	cfg := writeConfig(t, ssoSessionConfig) // defines [profile dev]

	_, err := LoadSSOSession(context.Background(), "ps", cfg)

	require.Error(t, err)
	msg := err.Error()
	require.Contains(t, msg, `profile "ps" not found`)
	require.Contains(t, msg, cfg)
	require.Contains(t, msg, "available profiles: dev")
	require.Contains(t, msg, "aws configure sso")
}

func TestLoadSSOSession_NoConfigFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "config") // never created

	_, err := LoadSSOSession(context.Background(), "dev", missing)

	require.Error(t, err)
	msg := err.Error()
	require.Contains(t, msg, "no AWS config file at "+missing)
	require.Contains(t, msg, "aws configure sso")
}

func TestLoadSSOSession_ProfileNotFound_BareHeaderHint(t *testing.T) {
	// Common mistake: [ps] instead of [profile ps] in ~/.aws/config.
	cfg := writeConfig(t, "[ps]\nsso_session = my-sso\n")

	_, err := LoadSSOSession(context.Background(), "ps", cfg)

	require.Error(t, err)
	require.Contains(t, err.Error(), "[profile ps]")
}

func TestLoadSSOSession_LegacyFormRejected(t *testing.T) {
	cfg := writeConfig(t, legacyOnlyConfig)

	_, err := LoadSSOSession(context.Background(), "legacy", cfg)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoSSOSession)
	require.Contains(t, err.Error(), "legacy")
}

func TestLoadSSOSession_NoSSOConfigured(t *testing.T) {
	cfg := writeConfig(t, noSSOConfig)

	_, err := LoadSSOSession(context.Background(), "plain", cfg)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoSSOSession)
}

func TestLoadSSOSession_MissingProfile(t *testing.T) {
	cfg := writeConfig(t, ssoSessionConfig)

	_, err := LoadSSOSession(context.Background(), "ghost", cfg)

	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
}

func TestLoadSSOSession_MissingSSORegion(t *testing.T) {
	cfg := writeConfig(t, malformedSessionConfig)

	_, err := LoadSSOSession(context.Background(), "dev", cfg)

	require.Error(t, err)
	require.Contains(t, err.Error(), "missing sso_region")
}
