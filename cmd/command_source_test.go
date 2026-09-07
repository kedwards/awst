package cmd

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kedwards/awst/v3/internal/runner"
	"github.com/kedwards/awst/v3/internal/tui"
)

func noPick(string) func([]string) (string, error) {
	return func([]string) (string, error) { return "", errors.New("picker not expected") }
}

func TestResolveCommandSource_Inline(t *testing.T) {
	got, err := resolveCommandSource(io.Discard, cmdSource{inline: "uptime"}, nil, nil, noPick(""))
	require.NoError(t, err)
	require.Equal(t, runner.Script{Body: "uptime"}, got)
}

func TestResolveCommandSource_FileParsesHeader(t *testing.T) {
	d := t.TempDir()
	p := writeFileT(t, d, "check", "# description: d\n# profile: prod\n# region: us-west-2\n# instances: web\necho hi\n", false)

	got, err := resolveCommandSource(io.Discard, cmdSource{file: p}, nil, nil, noPick(""))
	require.NoError(t, err)
	require.Equal(t, "prod", got.Profile)
	require.Equal(t, "us-west-2", got.Region)
	require.Equal(t, "web", got.Instances)
	require.Equal(t, "echo hi\n", got.Body)
}

func TestResolveCommandSource_NameResolvesFromDirs(t *testing.T) {
	base, user := t.TempDir(), t.TempDir()
	writeFileT(t, base, "dup", "echo base\n", false)
	writeFileT(t, user, "dup", "echo user\n", false)

	got, err := resolveCommandSource(io.Discard, cmdSource{name: "dup"}, []string{base, user}, nil, noPick(""))
	require.NoError(t, err)
	require.Equal(t, "echo user\n", got.Body, "later dirs win on collision")
}

func TestResolveCommandSource_MultipleSourcesRejected(t *testing.T) {
	for _, s := range []cmdSource{
		{inline: "a", file: "b"},
		{inline: "a", name: "b"},
		{file: "a", name: "b"},
	} {
		_, err := resolveCommandSource(io.Discard, s, nil, nil, noPick(""))
		require.Error(t, err)
		require.Contains(t, err.Error(), "only one of")
	}
}

func TestResolveCommandSource_NoSourceNonInteractiveListsAndErrors(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "saved", "echo hi\n", false)

	var out testWriter
	_, err := resolveCommandSource(&out, cmdSource{}, []string{d}, func() bool { return false }, noPick(""))
	require.ErrorIs(t, err, errNoCommand)
	require.Contains(t, out.String(), "saved")
}

func TestResolveCommandSource_NoSourceInteractivePicks(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "saved", "echo picked\n", false)

	got, err := resolveCommandSource(io.Discard, cmdSource{}, []string{d},
		func() bool { return true },
		func(names []string) (string, error) {
			require.Equal(t, []string{"saved"}, names)
			return "saved", nil
		})
	require.NoError(t, err)
	require.Equal(t, "echo picked\n", got.Body)
}

func TestResolveCommandSource_AbortedPickerPropagates(t *testing.T) {
	d := t.TempDir()
	writeFileT(t, d, "saved", "echo hi\n", false)

	_, err := resolveCommandSource(io.Discard, cmdSource{}, []string{d},
		func() bool { return true },
		func([]string) (string, error) { return "", tui.ErrAborted })
	require.ErrorIs(t, err, tui.ErrAborted)
}

func TestResolveCommandDirs_LayersBaseThenUser(t *testing.T) {
	base, user := t.TempDir(), t.TempDir()
	t.Setenv("AWST_CMD_DIR", "")
	t.Setenv("AWST_X_CMD_BASE", base)
	t.Setenv("AWST_X_CMD_USER", user)

	got, err := resolveCommandDirs("", "AWST_X", "/nonexistent")
	require.NoError(t, err)
	require.Equal(t, []string{base, user}, got)
}

func TestResolveCommandDirs_CustomIsExclusive(t *testing.T) {
	base, custom := t.TempDir(), t.TempDir()
	t.Setenv("AWST_CMD_DIR", "")
	t.Setenv("AWST_X_CMD_BASE", base)

	got, err := resolveCommandDirs(custom, "AWST_X", base)
	require.NoError(t, err)
	require.Equal(t, []string{custom}, got)
}

func TestResolveCommandDirs_EnvOverrideIsExclusive(t *testing.T) {
	base, env := t.TempDir(), t.TempDir()
	t.Setenv("AWST_CMD_DIR", env)
	t.Setenv("AWST_X_CMD_BASE", base)

	got, err := resolveCommandDirs("", "AWST_X", base)
	require.NoError(t, err)
	require.Equal(t, []string{env}, got)
}

func TestResolveCommandDirs_MissingDirErrors(t *testing.T) {
	t.Setenv("AWST_CMD_DIR", "")
	_, err := resolveCommandDirs("", "AWST_X", "/no/such/dir")
	require.Error(t, err)
}

type testWriter struct{ b []byte }

func (w *testWriter) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }
func (w *testWriter) String() string              { return string(w.b) }
