//go:build windows

package sessions

import (
	"fmt"
	"os"
)

func Kill(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}
	if err := p.Kill(); err != nil {
		return fmt.Errorf("kill process: %w", err)
	}
	return nil
}
