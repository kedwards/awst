package creds

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatExports_BasicCreds(t *testing.T) {
	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AKIA1",
		SecretAccessKey: "secret",
		SessionToken:    "token",
	})

	require.Contains(t, out, `export AWS_ACCESS_KEY_ID="AKIA1"`+"\n")
	require.Contains(t, out, `export AWS_SECRET_ACCESS_KEY="secret"`+"\n")
	require.Contains(t, out, `export AWS_SESSION_TOKEN="token"`+"\n")
}

func TestFormatExports_PreservesEqualsInTokens(t *testing.T) {
	// SDK / SSO session tokens are base64 and frequently contain '=' padding.
	// The bash awk parser at lib/commands/awst_creds.sh:61-68 splits on the
	// first '=' only — we must do the same.
	tokenWithEquals := "IQoJb3JpZ2luX2VjE==paddingABC=="

	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AKIA1",
		SecretAccessKey: "s",
		SessionToken:    tokenWithEquals,
	})

	require.Contains(t, out, `export AWS_SESSION_TOKEN="`+tokenWithEquals+`"`+"\n")
}

func TestFormatExports_IncludesShorthand(t *testing.T) {
	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AKIA1",
		SecretAccessKey: "secret",
		SessionToken:    "token",
	})

	require.Contains(t, out, `export AK="AKIA1"`+"\n")
	require.Contains(t, out, `export SK="secret"`+"\n")
	require.Contains(t, out, `export ST="token"`+"\n")
}

func TestFormatExports_IncludesAWSProfile(t *testing.T) {
	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AKIA1",
		SecretAccessKey: "s",
		SessionToken:    "t",
	})

	require.Contains(t, out, `export AWS_PROFILE="dev"`+"\n")
}

func TestFormatExports_OmitsRegionWhenEmpty(t *testing.T) {
	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AKIA1",
		SecretAccessKey: "s",
		SessionToken:    "t",
	})

	require.False(t, strings.Contains(out, "AWS_REGION"))
}

func TestFormatExports_IncludesRegionWhenSet(t *testing.T) {
	out := FormatExports("dev", Credentials{
		AccessKeyID:     "AKIA1",
		SecretAccessKey: "s",
		SessionToken:    "t",
		Region:          "us-east-1",
	})

	require.Contains(t, out, `export AWS_REGION="us-east-1"`+"\n")
}
