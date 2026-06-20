package creds

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatExports_Posix(t *testing.T) {
	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AKIA1",
		SecretAccessKey: "secret",
		SessionToken:    "token",
	}, ShellPosix)

	require.Contains(t, out, `export AWS_ACCESS_KEY_ID="AKIA1"`+"\n")
	require.Contains(t, out, `export AWS_SECRET_ACCESS_KEY="secret"`+"\n")
	require.Contains(t, out, `export AWS_SESSION_TOKEN="token"`+"\n")
	require.Contains(t, out, `export AWS_PROFILE="dev"`+"\n")
}

func TestFormatExports_PowerShell(t *testing.T) {
	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AKIA1",
		SecretAccessKey: "secret",
		SessionToken:    "token",
		Region:          "us-east-1",
	}, ShellPowerShell)

	require.Contains(t, out, `$env:AWS_ACCESS_KEY_ID = 'AKIA1'`+"\n")
	require.Contains(t, out, `$env:AWS_SECRET_ACCESS_KEY = 'secret'`+"\n")
	require.Contains(t, out, `$env:AWS_SESSION_TOKEN = 'token'`+"\n")
	require.Contains(t, out, `$env:AWS_REGION = 'us-east-1'`+"\n")
	require.Contains(t, out, `$env:AWS_PROFILE = 'dev'`+"\n")
	require.NotContains(t, out, "export ")
}

func TestFormatExports_PowerShellEscapesSingleQuote(t *testing.T) {
	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AK",
		SecretAccessKey: "ab'cd", // embedded single quote → doubled
		SessionToken:    "t",
	}, ShellPowerShell)

	require.Contains(t, out, `$env:AWS_SECRET_ACCESS_KEY = 'ab''cd'`+"\n")
}

func TestFormatExports_PreservesEqualsInTokens(t *testing.T) {
	// SSO session tokens are base64 and frequently contain '=' padding.
	tokenWithEquals := "IQoJb3JpZ2luX2VjE==paddingABC=="

	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AKIA1",
		SecretAccessKey: "s",
		SessionToken:    tokenWithEquals,
	}, ShellPosix)

	require.Contains(t, out, `export AWS_SESSION_TOKEN="`+tokenWithEquals+`"`+"\n")
}

func TestFormatExports_DropsAKSKSTShorthand(t *testing.T) {
	for _, shell := range []Shell{ShellPosix, ShellPowerShell} {
		out := FormatExports("dev", Credentials{AccessKeyID: "AKIA1", SecretAccessKey: "s", SessionToken: "t"}, shell)
		require.NotRegexp(t, `(?m)(export |\$env:)(AK|SK|ST)\b`, out, "shorthand should be gone (%s)", shell)
	}
}

func TestFormatExports_OmitsRegionWhenEmpty(t *testing.T) {
	out := FormatExports("dev", Credentials{AccessKeyID: "AKIA1", SecretAccessKey: "s", SessionToken: "t"}, ShellPosix)
	require.False(t, strings.Contains(out, "AWS_REGION"))
}

func TestFormatExports_IncludesRegionWhenSet(t *testing.T) {
	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AKIA1",
		SecretAccessKey: "s",
		SessionToken:    "t",
		Region:          "us-east-1",
	}, ShellPosix)
	require.Contains(t, out, `export AWS_REGION="us-east-1"`+"\n")
}

func TestParseShell(t *testing.T) {
	for _, in := range []string{"posix", "powershell"} {
		got, err := ParseShell(in)
		require.NoError(t, err)
		require.Equal(t, Shell(in), got)
	}
	_, err := ParseShell("cmd")
	require.Error(t, err)
}
