//go:build darwin

package sessions

import (
	"fmt"
	"os/exec"
)

// DefaultScan lists local session-manager-plugin processes via `ps`. macOS
// has no /proc; `ps -o command` reports the full command line space-joined.
// -ww disables truncation so the long StreamUrl arg survives.
func DefaultScan() ([]Session, error) {
	out, err := exec.Command("ps", "-axww", "-o", "pid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("list processes via ps: %w", err)
	}
	return parsePsOutput(out), nil
}
