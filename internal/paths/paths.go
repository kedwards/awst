package paths

import (
	"os"
	"path/filepath"
)

func CredsDir() string {
	if v := os.Getenv("AWST_CREDS_DIR"); v != "" {
		return v
	}
	return filepath.Join(os.Getenv("HOME"), ".local/share/aws-tools/creds")
}

// SSOCacheDir is where awst writes IAM Identity Center tokens. Pinned to
// ~/.aws/sso/cache because that's where the AWS SDK Go v2 token provider
// reads them back — the cache is the contract between `awst login` and
// `awst creds store`.
func SSOCacheDir() string {
	return filepath.Join(os.Getenv("HOME"), ".aws", "sso", "cache")
}
