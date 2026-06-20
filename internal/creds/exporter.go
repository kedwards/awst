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

// Shell selects the syntax FormatExports emits.
type Shell string

const (
	ShellPosix      Shell = "posix"      // export X="Y"  — eval "$(awst creds use dev)"
	ShellPowerShell Shell = "powershell" // $env:X = 'Y'  — awst creds use dev | iex
)

// ParseShell validates a --shell value.
func ParseShell(s string) (Shell, error) {
	switch Shell(s) {
	case ShellPosix:
		return ShellPosix, nil
	case ShellPowerShell:
		return ShellPowerShell, nil
	default:
		return "", fmt.Errorf("unknown shell %q (want posix or powershell)", s)
	}
}

// FormatExports returns shell statements that set the credential env vars
// for the given shell. posix output is consumed via eval "$(...)";
// powershell output via `... | iex`. cmd.exe has no clean eval equivalent
// and isn't supported — use PowerShell.
func FormatExports(profile string, c Credentials, shell Shell) string {
	vars := [][2]string{
		{"AWS_ACCESS_KEY_ID", c.AccessKeyID},
		{"AWS_SECRET_ACCESS_KEY", c.SecretAccessKey},
		{"AWS_SESSION_TOKEN", c.SessionToken},
	}
	if c.Region != "" {
		vars = append(vars, [2]string{"AWS_REGION", c.Region})
	}
	vars = append(vars, [2]string{"AWS_PROFILE", profile})

	var b strings.Builder
	for _, kv := range vars {
		if shell == ShellPowerShell {
			fmt.Fprintf(&b, "$env:%s = %s\n", kv[0], psQuote(kv[1]))
		} else {
			fmt.Fprintf(&b, "export %s=%q\n", kv[0], kv[1])
		}
	}
	return b.String()
}

// psQuote single-quotes a value for PowerShell. Single quotes are literal
// (no interpolation, so a '$' in a token is safe); an embedded single quote
// is escaped by doubling it.
func psQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}
