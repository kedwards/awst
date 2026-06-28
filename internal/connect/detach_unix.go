//go:build !windows

package connect

import "syscall"

// detachSysProcAttr starts the plugin in a new session (Setsid) so it has no
// controlling terminal and survives the parent shell hanging up (SIGHUP).
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
