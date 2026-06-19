package connect

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeConnFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "connections.config")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

func TestLoadConnections(t *testing.T) {
	// Mirrors the example connections.config shipped on the bash branch.
	body := `# comment
[LocalDB]
name = Database
host = localhost
port = 5432

[Engine]
name = CheckoutEngine
host = rds.internal
port = 5432
local_port = 15432

[Monitoring-All]
profile = ps
region = us-west-2
name = Monitoring
host = localhost
ports = 8428,9093
local_ports = 8428,9093
`
	conns, err := LoadConnections(writeConnFile(t, body))
	require.NoError(t, err)
	require.Len(t, conns, 3)

	db := conns["LocalDB"]
	require.Equal(t, "Database", db.Label)
	require.Equal(t, []PortForward{{Host: "localhost", LocalPort: "5432", RemotePort: "5432"}}, db.Forwards)

	eng := conns["Engine"]
	require.Equal(t, []PortForward{{Host: "rds.internal", LocalPort: "15432", RemotePort: "5432"}}, eng.Forwards)

	mon := conns["Monitoring-All"]
	require.Equal(t, "ps", mon.Profile)
	require.Equal(t, "us-west-2", mon.Region)
	require.Equal(t, []PortForward{
		{Host: "localhost", LocalPort: "8428", RemotePort: "8428"},
		{Host: "localhost", LocalPort: "9093", RemotePort: "9093"},
	}, mon.Forwards)
}

func TestLoadConnections_Errors(t *testing.T) {
	t.Run("missing port field", func(t *testing.T) {
		_, err := LoadConnections(writeConnFile(t, "[X]\nhost = localhost\n"))
		require.Error(t, err)
	})
	t.Run("ports/local_ports mismatch", func(t *testing.T) {
		_, err := LoadConnections(writeConnFile(t, "[X]\nports = 1,2\nlocal_ports = 1\n"))
		require.Error(t, err)
	})
	t.Run("key before any section", func(t *testing.T) {
		_, err := LoadConnections(writeConnFile(t, "port = 5432\n"))
		require.Error(t, err)
	})
	t.Run("missing file", func(t *testing.T) {
		_, err := LoadConnections(filepath.Join(t.TempDir(), "nope.config"))
		require.Error(t, err)
	})
}
