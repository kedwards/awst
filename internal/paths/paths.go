package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

func HomeDir() string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return home
	}
	return os.Getenv("HOME")
}

func ConfigDir() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	dir, err := os.UserConfigDir()
	if err == nil {
		return dir
	}
	home := HomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config")
}

func DataDir() string {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("APPDATA"); v != "" {
			return v
		}
		return ConfigDir()
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home := HomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share")
}

func AWSConfigFile() string {
	if v := os.Getenv("AWS_CONFIG_FILE"); v != "" {
		return v
	}
	return filepath.Join(HomeDir(), ".aws", "config")
}

func RunCommandsDir() string {
	return filepath.Join(ConfigDir(), "aws-tools", "commands", "aws")
}

func ConnectionsFile() string {
	return filepath.Join(ConfigDir(), "aws-tools", "connections.config")
}

func RegionsFile() string {
	if v := os.Getenv("AWST_REGIONS_FILE"); v != "" {
		return v
	}
	return filepath.Join(ConfigDir(), "aws-tools", "regions.config")
}

func CredsDir() string {
	if v := os.Getenv("AWST_CREDS_DIR"); v != "" {
		return v
	}
	return filepath.Join(DataDir(), "aws-tools", "creds")
}

// SSOCacheDir is where awst writes IAM Identity Center tokens. Pinned to
// ~/.aws/sso/cache because that's where the AWS SDK Go v2 token provider
// reads them back — the cache is the contract between `awst login` and
// `awst creds store`.
func SSOCacheDir() string {
	return filepath.Join(HomeDir(), ".aws", "sso", "cache")
}
