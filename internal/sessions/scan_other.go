//go:build !linux && !windows

package sessions

import (
	"fmt"
	"runtime"
)

// DefaultScan is unsupported off linux/windows. macOS would use
// `ps -axo pid,command`; not wired up until someone needs it.
func DefaultScan() ([]Session, error) {
	return nil, fmt.Errorf("listing local SSM sessions is not supported on %s", runtime.GOOS)
}
