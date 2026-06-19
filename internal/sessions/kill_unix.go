//go:build !windows

package sessions

import (
	"fmt"
	"syscall"
	"time"
)

// Kill sends SIGTERM, waits briefly, then SIGKILL if the process is
// still alive. ponytail: 250ms grace is empirical from the bash kill
// flow — bump if plugins start ignoring SIGTERM in practice.
func Kill(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("SIGTERM: %w", err)
	}
	time.Sleep(250 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err != nil {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("SIGKILL: %w", err)
	}
	return nil
}
