//go:build !linux

package sessions

import (
	"fmt"
	"runtime"
)

func DefaultScan() ([]Session, error) {
	return nil, fmt.Errorf("listing local SSM sessions is not supported on %s", runtime.GOOS)
}
