package connect

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// PortForward is one local→remote port mapping. Host is empty for a port
// on the instance itself (AWS-StartPortForwardingSession), or a hostname
// reachable from the instance — e.g. an RDS endpoint — for remote-host
// forwarding (AWS-StartPortForwardingSessionToRemoteHost).
type PortForward struct {
	Host       string
	LocalPort  string
	RemotePort string
}

const (
	docPortForward       = "AWS-StartPortForwardingSession"
	docPortForwardRemote = "AWS-StartPortForwardingSessionToRemoteHost"
)

func (pf PortForward) document() string {
	if pf.Host != "" {
		return docPortForwardRemote
	}
	return docPortForward
}

func (pf PortForward) parameters() map[string][]string {
	p := map[string][]string{
		"portNumber":      {pf.RemotePort},
		"localPortNumber": {pf.LocalPort},
	}
	if pf.Host != "" {
		p["host"] = []string{pf.Host}
	}
	return p
}

// String is the human label used in "Forwarding ..." log lines.
func (pf PortForward) String() string {
	dst := pf.RemotePort
	if pf.Host != "" {
		dst = pf.Host + ":" + pf.RemotePort
	}
	return fmt.Sprintf("localhost:%s -> %s", pf.LocalPort, dst)
}

// ParseForwardSpec parses a comma-separated list of port mappings. Each
// item is "PORT" (local==remote) or "LOCAL:REMOTE". host is applied to
// every mapping (remote-host forwarding when non-empty).
func ParseForwardSpec(spec, host string) ([]PortForward, error) {
	var out []PortForward
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		local, remote := item, item
		if i := strings.IndexByte(item, ':'); i >= 0 {
			local, remote = strings.TrimSpace(item[:i]), strings.TrimSpace(item[i+1:])
		}
		if err := validPort(local); err != nil {
			return nil, fmt.Errorf("local port %q: %w", local, err)
		}
		if err := validPort(remote); err != nil {
			return nil, fmt.Errorf("remote port %q: %w", remote, err)
		}
		out = append(out, PortForward{Host: host, LocalPort: local, RemotePort: remote})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no ports in forward spec %q", spec)
	}
	return out, nil
}

func validPort(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("not a number")
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("out of range 1-65535")
	}
	return nil
}

// StartPortForward starts an SSM port-forwarding session for pf against
// instanceID and hands the response to the plugin. Blocks until the
// session ends (Ctrl+C or remote close).
func StartPortForward(ctx context.Context, s SSMSessionClient, runner PluginRunner, pf PortForward, instanceID, region, profile, endpoint string) error {
	in := &ssm.StartSessionInput{
		Target:       aws.String(instanceID),
		DocumentName: aws.String(pf.document()),
		Parameters:   pf.parameters(),
	}
	out, err := s.StartSession(ctx, in)
	if err != nil {
		return fmt.Errorf("start ssm port-forward session: %w", err)
	}
	return dispatchToPlugin(runner, out, in, region, profile, endpoint)
}
