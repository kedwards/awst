package paths

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredsDir_DefaultsToXDG(t *testing.T) {
	t.Setenv("AWST_CREDS_DIR", "")
	setTestHome(t, "/home/fake")

	got := CredsDir()

	if runtime.GOOS == "windows" {
		require.Equal(t, filepath.Join("/home/fake", "AppData", "Roaming", "aws-tools", "creds"), got)
		return
	}
	require.Equal(t, filepath.Join("/home/fake", ".local/share/aws-tools/creds"), got)
}

func TestCredsDir_RespectsAWSTCredsDir(t *testing.T) {
	t.Setenv("AWST_CREDS_DIR", "/custom/path")
	setTestHome(t, "/home/fake")

	got := CredsDir()

	require.Equal(t, "/custom/path", got)
}

func TestSSOCacheDir_Pinned(t *testing.T) {
	setTestHome(t, "/home/fake")

	got := SSOCacheDir()

	require.Equal(t, filepath.Join("/home/fake", ".aws", "sso", "cache"), got)
}

func TestRunCommandsDir_DefaultsToConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")

	require.Equal(t, filepath.Join("/cfg", "aws-tools", "commands", "aws"), RunCommandsDir())
}

func TestDataDir_WindowsPrefersAppData(t *testing.T) {
	setTestHome(t, "/home/fake")
	t.Setenv("APPDATA", `C:\Users\fake\AppData\Roaming`)

	if runtime.GOOS == "windows" {
		require.Equal(t, `C:\Users\fake\AppData\Roaming`, DataDir())
		return
	}

	require.Equal(t, filepath.Join(HomeDir(), ".local", "share"), DataDir())
}

func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
}
