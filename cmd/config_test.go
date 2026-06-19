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

func runConfig(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newConfigCmd())
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func TestConfig_ReportsResolvedPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Clear anything inherited so the test is deterministic.
	for _, k := range []string{"AWST_CREDS_DIR", "AWST_CMD_DIR", "AWST_RUN_CMD_BASE", "AWST_RUN_CMD_USER", "AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION"} {
		t.Setenv(k, "")
	}

	out, _, err := runConfig(t, "config")
	require.NoError(t, err)

	// HOME-derived defaults show up.
	require.Contains(t, out, filepath.Join(home, ".local/share/aws-tools/creds"))
	require.Contains(t, out, filepath.Join(home, ".aws", "sso", "cache"))
	require.Contains(t, out, filepath.Join(home, ".config", "aws-tools", "commands", "aws"))
	require.Contains(t, out, filepath.Join(home, ".aws", "config"))
	// Unset AWS profile/region render as not-set, never blank.
	require.Contains(t, out, "(not set)")
}

func TestConfig_MarksMissingVsExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	credsDir := filepath.Join(home, "creds")
	require.NoError(t, os.MkdirAll(credsDir, 0o755))
	t.Setenv("AWST_CREDS_DIR", credsDir)

	out, _, err := runConfig(t, "config")
	require.NoError(t, err)

	// The dir we created is present; the SSO cache we did not create is missing.
	lines := strings.Split(out, "\n")
	var credsLine, ssoLine string
	for _, l := range lines {
		if strings.Contains(l, credsDir) {
			credsLine = l
		}
		if strings.Contains(l, filepath.Join(home, ".aws", "sso", "cache")) {
			ssoLine = l
		}
	}
	require.NotEmpty(t, credsLine)
	require.NotContains(t, credsLine, "missing")
	require.NotEmpty(t, ssoLine)
	require.Contains(t, ssoLine, "missing")
}

func TestConfig_HonorsOverridesAndAWSEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWST_RUN_CMD_USER", "/opt/awst/cmds")
	t.Setenv("AWS_PROFILE", "prod")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-1")

	out, _, err := runConfig(t, "config")
	require.NoError(t, err)

	require.Contains(t, out, "/opt/awst/cmds")
	require.Contains(t, out, "prod")
	require.Contains(t, out, "eu-west-1") // falls back to AWS_DEFAULT_REGION
}
