package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, name, body string, exec bool) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, name)
	mode := os.FileMode(0o644)
	if exec {
		mode = 0o755
	}
	require.NoError(t, os.WriteFile(p, []byte(body), mode))
	return p
}

func TestResolveDirs_ExclusiveD(t *testing.T) {
	d := t.TempDir()
	dirs, err := ResolveDirs(Options{D: d, Base: "/never/seen", User: "/never/seen"})
	require.NoError(t, err)
	require.Equal(t, []string{d}, dirs)
}

func TestResolveDirs_ExclusiveD_MissingErrors(t *testing.T) {
	_, err := ResolveDirs(Options{D: "/no/such/dir"})
	require.Error(t, err)
}

func TestResolveDirs_LayersBaseAndUser(t *testing.T) {
	base, user := t.TempDir(), t.TempDir()
	dirs, err := ResolveDirs(Options{Base: base, User: user})
	require.NoError(t, err)
	require.Equal(t, []string{base, user}, dirs, "base then user (user wins on collision)")
}

func TestResolveDirs_DropsMissingBaseAndUser(t *testing.T) {
	user := t.TempDir()
	dirs, err := ResolveDirs(Options{Base: "/no/such/base", User: user})
	require.NoError(t, err)
	require.Equal(t, []string{user}, dirs)
}

func TestResolveDirs_NoDirsAvailable(t *testing.T) {
	_, err := ResolveDirs(Options{Base: "/no/such/base", User: "/no/such/user"})
	require.Error(t, err)
}

func TestList_MergesAndSortsCommands(t *testing.T) {
	base, user := t.TempDir(), t.TempDir()
	writeFile(t, base, "vpc-cidrs", "#!/bin/sh\n# Show VPC CIDRs\naws ec2 describe-vpcs\n", false)
	writeFile(t, base, "instances", "#!/bin/sh\n# List instances\naws ec2 describe-instances\n", true)
	writeFile(t, user, "vpc-cidrs", "#!/bin/sh\n# OVERRIDDEN by user\naws ec2 describe-vpcs --output table\n", false)
	writeFile(t, user, "my-custom", "#!/bin/sh\n# My custom thing\necho hi\n", false)

	got, err := List([]string{base, user})
	require.NoError(t, err)
	require.Len(t, got, 3)

	byName := map[string]Command{}
	for _, c := range got {
		byName[c.Name] = c
	}
	require.Equal(t, "OVERRIDDEN by user", byName["vpc-cidrs"].Desc, "user dir wins on collision")
	// Executable bit is a unix concept; on windows Perm()&0o111 is always 0
	// (windows executable detection is the P2 run-semantics gap).
	if runtime.GOOS != "windows" {
		require.True(t, byName["instances"].Executable)
		require.False(t, byName["vpc-cidrs"].Executable)
	}

	require.Equal(t, "instances", got[0].Name, "sorted alphabetically")
	require.Equal(t, "my-custom", got[1].Name)
	require.Equal(t, "vpc-cidrs", got[2].Name)
}

func TestList_EmptyDirs(t *testing.T) {
	d := t.TempDir()
	got, err := List([]string{d})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestResolveScript_UserWinsOverBase(t *testing.T) {
	base, user := t.TempDir(), t.TempDir()
	wantedPath := writeFile(t, user, "snippet", "body", false)
	writeFile(t, base, "snippet", "old body", false)

	got, err := ResolveScript("snippet", []string{base, user})
	require.NoError(t, err)
	require.Equal(t, wantedPath, got)
}

func TestResolveScript_NotFound(t *testing.T) {
	d := t.TempDir()
	_, err := ResolveScript("ghost", []string{d})
	require.Error(t, err)
}

func TestLoadSnippet_StripsCommentsAndBlanks(t *testing.T) {
	d := t.TempDir()
	p := writeFile(t, d, "s", "# header comment\n# another\n\naws s3 ls\necho done\n", false)
	got, err := LoadSnippet(p)
	require.NoError(t, err)
	require.Equal(t, "aws s3 ls\necho done", got)
}

func TestSubstitute_ReplacesPlaceholders(t *testing.T) {
	got := Substitute("echo #ENV in #REGION", "dev", "us-east-1")
	require.Equal(t, "echo dev in us-east-1", got)
}

func TestSubstitute_AllOccurrences(t *testing.T) {
	got := Substitute("#ENV-#REGION-#ENV", "p", "r")
	require.Equal(t, "p-r-p", got)
}

func TestSubstitute_NoPlaceholdersPassesThrough(t *testing.T) {
	require.Equal(t, "aws s3 ls", Substitute("aws s3 ls", "dev", "us-east-1"))
}

func TestParseFilter_ProfileOnly(t *testing.T) {
	got, err := ParseFilter("dev prod")
	require.NoError(t, err)
	require.Equal(t, []Target{
		{Profile: "dev", Region: "us-east-1"},
		{Profile: "prod", Region: "us-east-1"},
	}, got)
}

func TestParseFilter_ProfileWithRegion(t *testing.T) {
	got, err := ParseFilter("dev:us-east-2 prod:eu-west-1")
	require.NoError(t, err)
	require.Equal(t, []Target{
		{Profile: "dev", Region: "us-east-2"},
		{Profile: "prod", Region: "eu-west-1"},
	}, got)
}

func TestParseFilter_Mixed(t *testing.T) {
	got, err := ParseFilter("dev prod:eu-west-1")
	require.NoError(t, err)
	require.Equal(t, []Target{
		{Profile: "dev", Region: "us-east-1"},
		{Profile: "prod", Region: "eu-west-1"},
	}, got)
}

func TestParseFilter_Empty(t *testing.T) {
	got, err := ParseFilter("")
	require.NoError(t, err)
	require.Empty(t, got)
	got, err = ParseFilter("   ")
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestParseFilter_RejectsShellMetachars(t *testing.T) {
	_, err := ParseFilter("prod;echo pwned")
	require.Error(t, err)
	_, err = ParseFilter("dev$(whoami)")
	require.Error(t, err)
	_, err = ParseFilter("ok:us-east-1|evil")
	require.Error(t, err)
}
