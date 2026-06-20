package sessions

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

// parsePsOutput parses `ps -o pid=,command=` lines into Sessions. Each line
// is leading-space + PID + space + the full command line. The plugin's args
// are all space-free tokens (compact JSON, region, op, profile, endpoint),
// so whitespace-splitting the command faithfully reconstructs argv — which
// `ps` reports space-joined, unlike Linux's NUL-separated /proc cmdline.
func parsePsOutput(out []byte) []Session {
	var sessions []Session
	s := bufio.NewScanner(bytes.NewReader(out))
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024) // StreamUrl arg can be long
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		sess, ok := ParseArgs(fields[1:])
		if !ok {
			continue
		}
		sess.PID = pid
		sessions = append(sessions, sess)
	}
	return sessions
}
