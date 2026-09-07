package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestLoad_ParsesHeaderFields(t *testing.T) {
	path := writeTemp(t, "barx-rate-check", `# description: BARX vendor connectivity check
# profile: rch-platform-dev-coffee
# region: us-east-1
# instances: i-0823f48e03ca63c37
echo hi
`)
	s, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "BARX vendor connectivity check", s.Desc)
	require.Equal(t, "rch-platform-dev-coffee", s.Profile)
	require.Equal(t, "us-east-1", s.Region)
	require.Equal(t, "i-0823f48e03ca63c37", s.Instances)
	require.Equal(t, "echo hi\n", s.Body)
	require.Equal(t, "barx-rate-check", s.Name)
	require.Equal(t, path, s.Path)
}

func TestLoad_PreservesHeredocBodyVerbatim(t *testing.T) {
	body := `cat > /tmp/req.xml <<EOF
<?xml version="1.0"?>
# not a header, this is inside the heredoc

<Foo/>
EOF
curl -sS https://example.com
rm -f /tmp/req.xml
`
	path := writeTemp(t, "script", "# profile: dev\n"+body)
	s, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "dev", s.Profile)
	require.Equal(t, body, s.Body)
}

func TestLoad_NoHeader_BodyOnly(t *testing.T) {
	path := writeTemp(t, "plain", "echo hello\necho world\n")
	s, err := Load(path)
	require.NoError(t, err)
	require.Empty(t, s.Desc)
	require.Empty(t, s.Profile)
	require.Empty(t, s.Region)
	require.Empty(t, s.Instances)
	require.Equal(t, "echo hello\necho world\n", s.Body)
}

func TestLoad_KeepsShebangAndUnrecognizedComments(t *testing.T) {
	path := writeTemp(t, "script", `#!/bin/sh
# profile: dev
# some other comment
echo hi
`)
	s, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "dev", s.Profile)
	require.Equal(t, "#!/bin/sh\n# some other comment\necho hi\n", s.Body)
}

func TestLoad_HeaderBlockEndsAtFirstBlankOrNonCommentLine(t *testing.T) {
	path := writeTemp(t, "script", `# profile: dev

# region: us-east-1
echo hi
`)
	s, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "dev", s.Profile)
	// region= comes after the blank line, so it's not part of the header
	// block and is preserved as body content instead.
	require.Empty(t, s.Region)
	require.Equal(t, "\n# region: us-east-1\necho hi\n", s.Body)
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
}
