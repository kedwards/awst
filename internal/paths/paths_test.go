package paths

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredsDir_DefaultsToXDG(t *testing.T) {
	t.Setenv("AWST_CREDS_DIR", "")
	t.Setenv("HOME", "/home/fake")

	got := CredsDir()

	require.Equal(t, filepath.Join("/home/fake", ".local/share/aws-tools/creds"), got)
}

func TestCredsDir_RespectsAWSTCredsDir(t *testing.T) {
	t.Setenv("AWST_CREDS_DIR", "/custom/path")
	t.Setenv("HOME", "/home/fake")

	got := CredsDir()

	require.Equal(t, "/custom/path", got)
}

func TestSSOCacheDir_Pinned(t *testing.T) {
	t.Setenv("HOME", "/home/fake")

	got := SSOCacheDir()

	require.Equal(t, filepath.Join("/home/fake", ".aws", "sso", "cache"), got)
}
