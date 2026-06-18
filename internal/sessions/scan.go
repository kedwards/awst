// Package sessions inspects the local OS for active SSM
// session-manager-plugin processes started by awst connect. Linux-only;
// the macOS upgrade path is ps -E (BSD) or libproc — neither is needed
// until someone tries to use awst list on darwin.
//
// ponytail: /proc-only. Add darwin support when the user runs on it.
package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kedwards/aws-tools/internal/connect"
)

// Session is one running session-manager-plugin invocation.
type Session struct {
	PID     int
	Type    string
	Target  string
	Region  string
	Profile string
}

// ParseArgs interprets the argv of a process. If the basename of argv[0]
// is session-manager-plugin and the argv shape matches what `awst connect`
// (and the AWS CLI) passes, returns the Session and true. The PID is left
// zero; Scan fills it in.
func ParseArgs(argv []string) (Session, bool) {
	if len(argv) < 7 {
		return Session{}, false
	}
	if filepath.Base(argv[0]) != connect.PluginName {
		return Session{}, false
	}
	s := Session{
		Region:  argv[2],
		Profile: argv[4],
		Type:    sessionType(argv[3]),
		Target:  parseTarget(argv[5]),
	}
	return s, true
}

func sessionType(operation string) string {
	switch operation {
	case "StartSession":
		return "shell"
	case "StartPortForwardingSession", "StartPortForwardingSessionToRemoteHost":
		return "port-forward"
	default:
		return strings.ToLower(operation)
	}
}

func parseTarget(paramsJSON string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(paramsJSON), &m); err != nil {
		return ""
	}
	for _, k := range []string{"Target", "TargetId"} {
		if v, ok := m[k].(string); ok {
			return v
		}
	}
	return ""
}

// Scan walks procRoot (typically /proc) and returns a Session per running
// session-manager-plugin process. Unreadable PID dirs are skipped silently
// — the only thing that fails the whole call is a missing procRoot.
func Scan(procRoot string) ([]Session, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", procRoot, err)
	}
	var out []Session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		argv := splitCmdline(raw)
		s, ok := ParseArgs(argv)
		if !ok {
			continue
		}
		s.PID = pid
		out = append(out, s)
	}
	return out, nil
}

// splitCmdline splits /proc/<pid>/cmdline (NUL-separated, NUL-terminated)
// into its argv fields. A trailing empty field is dropped.
func splitCmdline(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	parts := strings.Split(string(b), "\x00")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}
