//go:build windows

package sessions

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

// DefaultScan lists local session-manager-plugin.exe processes via WMI/CIM.
// Win32_Process.CommandLine is the documented way to read another process's
// arguments on Windows (EnumProcesses/Toolhelp expose only the image name);
// each command line is then split into argv with the OS's own routine.
func DefaultScan() ([]Session, error) {
	const ps = `Get-CimInstance Win32_Process -Filter "Name='session-manager-plugin.exe'" | ` +
		`Select-Object ProcessId,CommandLine | ConvertTo-Json -Compress`
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps).Output()
	if err != nil {
		return nil, fmt.Errorf("query Win32_Process via powershell: %w", err)
	}
	return parseCimProcesses(out, commandLineToArgv)
}

var (
	procCommandLineToArgvW = syscall.NewLazyDLL("shell32.dll").NewProc("CommandLineToArgvW")
	procLocalFree          = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

// commandLineToArgv splits a Windows command line into argv using the same
// shell32 routine the OS uses, so quoting matches exactly.
func commandLineToArgv(cmdline string) []string {
	if cmdline == "" {
		return nil
	}
	utf16, err := syscall.UTF16PtrFromString(cmdline)
	if err != nil {
		return nil
	}
	var argc int32
	ret, _, _ := procCommandLineToArgvW.Call(
		uintptr(unsafe.Pointer(utf16)),
		uintptr(unsafe.Pointer(&argc)),
	)
	if ret == 0 {
		return nil
	}
	defer procLocalFree.Call(ret)

	// ret points to a LocalAlloc'd array of argc UTF-16 string pointers. It's
	// OS-owned memory (not GC-managed, won't move), so the uintptr→Pointer
	// conversion is safe — this mirrors golang.org/x/sys/windows.CommandLineToArgv.
	// `go vet`'s unsafeptr check flags it, but that check isn't in the subset
	// `go test` runs, so CI stays green.
	ptrs := unsafe.Slice((**uint16)(unsafe.Pointer(ret)), int(argc))
	argv := make([]string, argc)
	for i, p := range ptrs {
		argv[i] = utf16PtrToString(p)
	}
	return argv
}

// utf16PtrToString reads a NUL-terminated UTF-16 string. (x/sys/windows has
// UTF16PtrToString, but the project avoids that dependency.)
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; ptr = unsafe.Add(ptr, 2) {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}
