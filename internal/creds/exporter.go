package creds

import (
	"fmt"
	"strings"
)

type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Region          string
}

// FormatExports returns shell `export` statements consumed by
// `eval "$(awst creds store|use <profile>)"`. The output is the contract
// the bash version emits in lib/commands/awst_creds.sh — including the
// AK/SK/ST shorthand for backwards-compat.
func FormatExports(profile string, c Credentials) string {
	var b strings.Builder
	line := func(k, v string) {
		fmt.Fprintf(&b, "export %s=%q\n", k, v)
	}
	line("AWS_ACCESS_KEY_ID", c.AccessKeyID)
	line("AWS_SECRET_ACCESS_KEY", c.SecretAccessKey)
	line("AWS_SESSION_TOKEN", c.SessionToken)
	if c.Region != "" {
		line("AWS_REGION", c.Region)
	}
	line("AWS_PROFILE", profile)
	line("AK", c.AccessKeyID)
	line("SK", c.SecretAccessKey)
	line("ST", c.SessionToken)
	return b.String()
}
