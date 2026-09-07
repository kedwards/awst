package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Script is a saved awst command file, shared by `awst exec` and `awst run`:
// an optional leading block of "# key: value" header comments
// (description/profile/region/instances) followed by the body, which is sent
// verbatim to the shell (AWS-RunShellScript for exec, `sh -c` for run).
type Script struct {
	Name      string
	Path      string
	Desc      string
	Profile   string
	Region    string
	Instances string
	Body      string
}

// Load reads path and splits it into header fields + body. Only a
// contiguous run of "# key: value" comments at the top of the file (after
// an optional shebang line) is treated as a header; the first line that
// isn't a recognized header comment — blank, code, or an unrecognized
// comment — ends header parsing, and it plus everything after it is kept
// as Body exactly as written, so heredocs, embedded "#" text, and blank
// lines in the body survive untouched.
func Load(path string) (Script, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Script{}, fmt.Errorf("read %s: %w", path, err)
	}
	s := Script{Name: filepath.Base(path), Path: path}

	content := string(raw)
	trailingNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(content, "\n")
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}

	var bodyLines []string
	i := 0
	if i < len(lines) && strings.HasPrefix(lines[i], "#!") {
		bodyLines = append(bodyLines, lines[i])
		i++
	}

headerLoop:
	for ; i < len(lines); i++ {
		key, value, ok := parseHeaderLine(lines[i])
		if !ok {
			break headerLoop
		}
		switch key {
		case "description":
			s.Desc = value
		case "profile":
			s.Profile = value
		case "region":
			s.Region = value
		case "instances":
			s.Instances = value
		default:
			break headerLoop
		}
	}

	bodyLines = append(bodyLines, lines[i:]...)
	s.Body = strings.Join(bodyLines, "\n")
	if trailingNewline {
		s.Body += "\n"
	}
	return s, nil
}

// parseHeaderLine reports whether line is a "# key: value" header comment,
// returning the trimmed key and trimmed value.
func parseHeaderLine(line string) (key, value string, ok bool) {
	if !strings.HasPrefix(line, "#") {
		return "", "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "#"))
	k, v, found := strings.Cut(rest, ":")
	if !found {
		return "", "", false
	}
	return strings.TrimSpace(k), strings.TrimSpace(v), true
}
