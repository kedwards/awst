package connect

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Connection is one named saved port-forward parsed from an INI-style
// connections file. A section's `name` field (Label) is the instance
// Name-tag filter used to pick the bastion/target; `host` is the endpoint
// reachable from it (e.g. an RDS host) — empty means a port on the
// instance itself.
type Connection struct {
	Name     string // section header, e.g. "Engine"
	Label    string // optional `name =` field, used as the instance filter
	Profile  string
	Region   string
	URL      string
	Forwards []PortForward
}

// LoadConnections parses an INI-style connections file. Each [section] is a
// named port-forward. Recognized keys: profile, region, host, port, ports,
// local_port, local_ports, url, name. Matches the bash connections.config
// format so existing files work unchanged.
func LoadConnections(path string) (map[string]Connection, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sections := map[string]map[string]string{}
	var order []string
	cur := ""

	s := bufio.NewScanner(f)
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = strings.TrimSpace(line[1 : len(line)-1])
			if cur == "" {
				return nil, fmt.Errorf("line %d: empty section name", lineNo)
			}
			if _, ok := sections[cur]; !ok {
				sections[cur] = map[string]string{}
				order = append(order, cur)
			}
			continue
		}
		if cur == "" {
			return nil, fmt.Errorf("line %d: key outside any [section]", lineNo)
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value, got %q", lineNo, line)
		}
		sections[cur][strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}

	out := make(map[string]Connection, len(order))
	for _, name := range order {
		c, err := buildConnection(name, sections[name])
		if err != nil {
			return nil, fmt.Errorf("connection %q: %w", name, err)
		}
		out[name] = c
	}
	return out, nil
}

func buildConnection(name string, f map[string]string) (Connection, error) {
	forwards, err := expandPorts(f["host"], f)
	if err != nil {
		return Connection{}, err
	}
	return Connection{
		Name:     name,
		Label:    f["name"],
		Profile:  f["profile"],
		Region:   f["region"],
		URL:      f["url"],
		Forwards: forwards,
	}, nil
}

// expandPorts turns the port(s)/local_port(s) fields into PortForwards.
// `ports`/`local_ports` are comma-separated; the single `port`/`local_port`
// form is the back-compatible default. local defaults to remote.
func expandPorts(host string, f map[string]string) ([]PortForward, error) {
	var remote, local []string
	switch {
	case f["ports"] != "":
		remote = splitCSV(f["ports"])
		if local = splitCSV(f["local_ports"]); len(local) == 0 {
			local = remote
		}
	case f["port"] != "":
		remote = []string{f["port"]}
		if lp := f["local_port"]; lp != "" {
			local = []string{lp}
		} else {
			local = remote
		}
	default:
		return nil, fmt.Errorf("no port or ports field")
	}
	if len(local) != len(remote) {
		return nil, fmt.Errorf("local_ports/ports length mismatch (%d vs %d)", len(local), len(remote))
	}
	out := make([]PortForward, 0, len(remote))
	for i := range remote {
		if err := validPort(remote[i]); err != nil {
			return nil, fmt.Errorf("remote port %q: %w", remote[i], err)
		}
		if err := validPort(local[i]); err != nil {
			return nil, fmt.Errorf("local port %q: %w", local[i], err)
		}
		out = append(out, PortForward{Host: host, LocalPort: local[i], RemotePort: remote[i]})
	}
	return out, nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
