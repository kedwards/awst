//go:build windows

package connect

import "syscall"

// CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS — defined locally to avoid
// pulling in golang.org/x/sys/windows for two constants.
const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detachSysProcAttr starts the plugin detached from the console so it survives
// the parent shell closing.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
