package runner

import "os/exec"

// POSIXShell returns the sh executable used to run snippets and inline
// commands. The snippet library is POSIX shell (it uses `\`-continuations,
// `$(...)`, pipes, jq), so awst runs it via `sh -c`.
func POSIXShell() (string, error) {
	return posixShell(exec.LookPath)
}
func posixShell(lookPath func(string) (string, error)) (string, error) {
	if p, err := lookPath("sh"); err == nil {
		return p, nil
	}
	return "sh", nil
}
