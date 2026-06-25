// Package regions holds the user-configurable list of AWS regions awst offers
// in its interactive region picker. A built-in Default list makes the picker
// work with no setup; `awst config regions add/remove` writes a user list that
// takes over once non-empty.
package regions

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Default is the built-in region list used until the user configures their own.
var Default = []string{
	"us-east-1", "us-east-2", "us-west-1", "us-west-2",
	"ca-central-1",
	"eu-west-1", "eu-west-2", "eu-west-3", "eu-central-1", "eu-north-1",
	"ap-south-1", "ap-southeast-1", "ap-southeast-2",
	"ap-northeast-1", "ap-northeast-2",
	"sa-east-1",
}

// Load reads the user region list (one region per line, # comments and blank
// lines ignored). A missing file is not an error — it returns nil.
func Load(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		out = append(out, line)
	}
	return out, s.Err()
}

// Effective returns the user list when it has any entries, otherwise Default.
func Effective(path string) ([]string, error) {
	user, err := Load(path)
	if err != nil {
		return nil, err
	}
	if len(user) > 0 {
		return user, nil
	}
	return Default, nil
}

// Add appends region to the user list (creating the file/dir if needed) and
// returns true if it was newly added (false if already present).
func Add(path, region string) (bool, error) {
	list, err := Load(path)
	if err != nil {
		return false, err
	}
	for _, r := range list {
		if r == region {
			return false, nil
		}
	}
	list = append(list, region)
	sort.Strings(list)
	return true, save(path, list)
}

// Remove drops region from the user list and returns true if it was present.
func Remove(path, region string) (bool, error) {
	list, err := Load(path)
	if err != nil {
		return false, err
	}
	out := list[:0:0]
	found := false
	for _, r := range list {
		if r == region {
			found = true
			continue
		}
		out = append(out, r)
	}
	if !found {
		return false, nil
	}
	return true, save(path, out)
}

// save writes the region list, one per line, creating the parent directory.
// ponytail: plain truncating write, no atomic rename — this file is tiny and
// only touched by interactive config edits; add a temp-file swap if it ever
// gets written concurrently.
func save(path string, list []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	for _, r := range list {
		b.WriteString(r)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
