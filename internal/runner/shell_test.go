package runner

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func notFound(string) (string, error) { return "", errors.New("not found") }

func TestPOSIXShell_Unix(t *testing.T) {
	got, err := posixShell("linux", notFound)
	require.NoError(t, err)
	require.Equal(t, "sh", got)
}

func TestPOSIXShell_WindowsFindsSh(t *testing.T) {
	lp := func(name string) (string, error) {
		if name == "sh" {
			return `C:\Git\usr\bin\sh.exe`, nil
		}
		return "", errors.New("not found")
	}
	got, err := posixShell("windows", lp)
	require.NoError(t, err)
	require.Equal(t, `C:\Git\usr\bin\sh.exe`, got)
}

func TestPOSIXShell_WindowsFallsBackToBash(t *testing.T) {
	lp := func(name string) (string, error) {
		if name == "bash" {
			return `C:\Git\bin\bash.exe`, nil
		}
		return "", errors.New("not found")
	}
	got, err := posixShell("windows", lp)
	require.NoError(t, err)
	require.Equal(t, `C:\Git\bin\bash.exe`, got)
}

func TestPOSIXShell_WindowsNoShellErrors(t *testing.T) {
	got, err := posixShell("windows", notFound)
	require.Error(t, err)
	require.Empty(t, got)
	require.Contains(t, err.Error(), "POSIX shell")
}
