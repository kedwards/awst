// Package runner backs `awst run`: command discovery (snippet files +
// executable scripts under one or more directories), placeholder
// substitution, and target-filter parsing. Profile-iteration + child-
// process exec live in cmd/run.go where they have side effects.
package runner

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultRegion = "us-east-1"

// Options is the input to ResolveDirs. The fields mirror the bash layering:
// D (or env equiv) is an exclusive override; otherwise Base + User merge
// with User winning on collisions.
type Options struct {
	D    string // explicit -d flag or AWST_CMD_DIR env var
	Base string // shipped defaults (AWST_RUN_CMD_BASE)
	User string // per-user customizations (AWST_RUN_CMD_USER)
}

// ResolveDirs returns the ordered list of directories to search. If D is
// set, it is the only directory (and must exist). Otherwise Base then User
// are returned, in that priority order (later entries override earlier
// ones). Returns an error if no usable directory is configured.
func ResolveDirs(o Options) ([]string, error) {
	if o.D != "" {
		if !isDir(o.D) {
			return nil, fmt.Errorf("commands directory not found: %s", o.D)
		}
		return []string{o.D}, nil
	}
	var out []string
	if isDir(o.Base) {
		out = append(out, o.Base)
	}
	if isDir(o.User) && o.User != o.Base {
		out = append(out, o.User)
	}
	if len(out) == 0 {
		return nil, errors.New("no commands directory configured (set AWST_CMD_DIR / AWST_RUN_CMD_BASE / AWST_RUN_CMD_USER or pass -d)")
	}
	return out, nil
}

func isDir(p string) bool {
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// Command is one discoverable command.
type Command struct {
	Name       string
	Desc       string // first non-shebang comment line
	Path       string // resolved path (from the highest-priority dir)
	Executable bool
}

// List merges commands across dirs in priority order; later dirs override
// earlier ones on name collision. Result is sorted alphabetically.
func List(dirs []string) ([]Command, error) {
	byName := map[string]Command{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			path := filepath.Join(dir, e.Name())
			byName[e.Name()] = Command{
				Name:       e.Name(),
				Desc:       readDescription(path),
				Path:       path,
				Executable: info.Mode().Perm()&0o111 != 0,
			}
		}
	}
	out := make([]Command, 0, len(byName))
	for _, c := range byName {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// readDescription returns the first non-shebang comment line, stripped of
// its leading "# " — matching the bash version's `sed -n '2s/^# *//p'`
// (line 2 after the shebang).
func readDescription(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	lineNum := 0
	for s.Scan() {
		lineNum++
		line := s.Text()
		if lineNum == 1 && strings.HasPrefix(line, "#!") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimPrefix(line, "#"))
		}
		return ""
	}
	return ""
}

// ResolveScript looks up name across dirs, with later dirs winning on
// collision. Returns the path of the first hit or an error if no dir
// contains the name.
func ResolveScript(name string, dirs []string) (string, error) {
	for i := len(dirs) - 1; i >= 0; i-- {
		p := filepath.Join(dirs[i], name)
		info, err := os.Stat(p)
		if err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("command %q not found in any commands directory", name)
}

// LoadSnippet reads a snippet file, stripping comment lines and blank
// lines (matching bash `sed '/^#/d; /^$/d'`).
func LoadSnippet(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

// Substitute replaces #ENV and #REGION placeholders. Kept for back-compat
// with the bash snippet library — new snippets can just use $AWS_PROFILE
// and $AWS_REGION since those are exported into the child env.
func Substitute(cmd, profile, region string) string {
	cmd = strings.ReplaceAll(cmd, "#ENV", profile)
	cmd = strings.ReplaceAll(cmd, "#REGION", region)
	return cmd
}

// Target is one profile/region to run against.
type Target struct {
	Profile string
	Region  string
}

// ParseFilter parses a space-separated filter argument: each token is
// either "profile" (default region) or "profile:region".
func ParseFilter(s string) []Target {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	out := make([]Target, 0, len(fields))
	for _, f := range fields {
		t := Target{Region: defaultRegion}
		if i := strings.IndexByte(f, ':'); i >= 0 {
			t.Profile = f[:i]
			t.Region = f[i+1:]
		} else {
			t.Profile = f
		}
		out = append(out, t)
	}
	return out
}
