package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShellCommandArgsFor_Unix(t *testing.T) {
	require.Equal(t, []string{"sh", "-c", "echo hi"}, shellCommandArgsFor("linux", "", "echo hi"))
}

func TestShellCommandArgsFor_WindowsDefault(t *testing.T) {
	require.Equal(t, []string{"cmd.exe", "/C", "dir"}, shellCommandArgsFor("windows", "", "dir"))
}

func TestShellCommandArgsFor_WindowsRespectsComspec(t *testing.T) {
	require.Equal(t, []string{"C:\\Windows\\System32\\cmd.exe", "/C", "dir"}, shellCommandArgsFor("windows", "C:\\Windows\\System32\\cmd.exe", "dir"))
}
