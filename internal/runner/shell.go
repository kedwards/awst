package runner

import (
	"os"
	"runtime"
)

func ShellCommandArgs(command string) []string {
	return shellCommandArgsFor(runtime.GOOS, os.Getenv("COMSPEC"), command)
}

func shellCommandArgsFor(goos, comspec, command string) []string {
	if goos == "windows" {
		if comspec == "" {
			comspec = "cmd.exe"
		}
		return []string{comspec, "/C", command}
	}
	return []string{"sh", "-c", command}
}
