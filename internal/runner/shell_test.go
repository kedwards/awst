package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPOSIXShell_ReturnsSh(t *testing.T) {
	lp := func(name string) (string, error) {
		require.Equal(t, "sh", name)
		return "/bin/sh", nil
	}
	got, err := posixShell(lp)
	require.NoError(t, err)
	require.Equal(t, "/bin/sh", got)
}
