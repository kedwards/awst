//go:build windows

package sessions

import (
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// Exercises the real shell32 CommandLineToArgvW syscall — runs only on the
// windows CI runner, where it actually matters.

// TestCommandLineToArgv_RoundTrip mirrors how a command line is actually
// formed: os/exec escapes each arg with syscall.EscapeArg, and the running
// process's WMI CommandLine is that escaped string. commandLineToArgv must
// reverse it back to the original argv — including JSON args full of quotes.
func TestCommandLineToArgv_RoundTrip(t *testing.T) {
	args := []string{
		`C:\bin\session-manager-plugin.exe`,
		`{"SessionId":"s","StreamUrl":"wss://x","TokenValue":"t"}`,
		"us-east-1",
		"StartSession",
		"dev",
		`{"Target":"i-1"}`,
		"https://ssm.us-east-1.amazonaws.com",
	}
	escaped := make([]string, len(args))
	for i, a := range args {
		escaped[i] = syscall.EscapeArg(a)
	}
	require.Equal(t, args, commandLineToArgv(strings.Join(escaped, " ")))
}

func TestCommandLineToArgv_Quoting(t *testing.T) {
	require.Equal(t, []string{"prog.exe", "a b", "c"}, commandLineToArgv(`prog.exe "a b" c`))
}

func TestCommandLineToArgv_Empty(t *testing.T) {
	require.Nil(t, commandLineToArgv(""))
}
