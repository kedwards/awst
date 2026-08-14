//go:build !linux && !darwin

package sessions

import (
	"fmt"
	"runtime"
)

// DefaultScan is unsupported on platforms other than linux/darwin.
func DefaultScan() ([]Session, error) {
	return nil, fmt.Errorf("listing local SSM sessions is not supported on %s", runtime.GOOS)
}
