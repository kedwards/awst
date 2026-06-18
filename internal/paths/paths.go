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
