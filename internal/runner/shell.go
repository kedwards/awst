package runner

import (
	"errors"
	"os/exec"
	"runtime"
)

// POSIXShell returns the sh/bash executable used to run snippets and inline
// commands. The snippet library is POSIX shell on every platform (it uses
// `\`-continuations, `$(...)`, pipes, jq), so it runs via `sh -c` — cmd.exe
// and PowerShell can't execute it. On Windows we locate sh/bash on PATH
// (Git Bash / WSL / MSYS) and return an actionable error if none is found.
func POSIXShell() (string, error) {
	return posixShell(runtime.GOOS, exec.LookPath)
}

func posixShell(goos string, lookPath func(string) (string, error)) (string, error) {
	if goos != "windows" {
		return "sh", nil
	}
	for _, name := range []string{"sh", "bash"} {
		if p, err := lookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("awst run needs a POSIX shell (sh or bash) on PATH to run snippets — install Git Bash or WSL")
}
