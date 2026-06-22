package sso

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/stretchr/testify/require"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	return NewCache(filepath.Join(t.TempDir(), "cache"))
}

func TestCache_Delete_RemovesTokenAndIsIdempotent(t *testing.T) {
	c := newTestCache(t)
	require.NoError(t, c.Save("sess", Token{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)}))
	require.NoError(t, c.Delete("sess"))
	_, err := os.Stat(c.Path("sess"))
	require.True(t, os.IsNotExist(err))
	require.NoError(t, c.Delete("sess"), "deleting a missing token is not an error")
}

func TestCache_DeleteAll(t *testing.T) {
	c := newTestCache(t)
	require.NoError(t, c.Save("a", Token{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)}))
	require.NoError(t, c.Save("b", Token{AccessToken: "y", ExpiresAt: time.Now().Add(time.Hour)}))

	n, err := c.DeleteAll()
	require.NoError(t, err)
	require.Equal(t, 2, n)

	// Missing cache dir is clean (zero, no error).
	n, err = NewCache(filepath.Join(t.TempDir(), "absent")).DeleteAll()
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

func TestCache_Save_WritesFileWithMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes aren't represented on windows")
	}
	c := newTestCache(t)

	require.NoError(t, c.Save("my-sso", Token{
		AccessToken:  "atk",
		ExpiresAt:    time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC),
		RefreshToken: "rt",
		ClientID:     "cid",
		ClientSecret: "csec",
	}))

	info, err := os.Stat(c.Path("my-sso"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCache_Save_WritesSDKReadableJSON(t *testing.T) {
	c := newTestCache(t)
	exp := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	require.NoError(t, c.Save("my-sso", Token{
		AccessToken:  "atk",
		ExpiresAt:    exp,
		RefreshToken: "rt",
		ClientID:     "cid",
		ClientSecret: "csec",
	}))

	raw, err := os.ReadFile(c.Path("my-sso"))
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, "atk", got["accessToken"])
	require.Equal(t, "rt", got["refreshToken"])
	require.Equal(t, "cid", got["clientId"])
	require.Equal(t, "csec", got["clientSecret"])
	require.Equal(t, exp.Format(time.RFC3339), got["expiresAt"])
}

func TestCache_Path_MatchesSDKStandardLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")

	want, err := ssocreds.StandardCachedTokenFilepath("my-sso")
	require.NoError(t, err)

	c := NewCache(filepath.Join(home, ".aws", "sso", "cache"))
	require.Equal(t, want, c.Path("my-sso"))
}

func TestCache_Save_CreatesDirWithMode0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes aren't represented on windows")
	}
	c := newTestCache(t)
	require.NoError(t, c.Save("my-sso", Token{AccessToken: "atk", ExpiresAt: time.Now()}))

	info, err := os.Stat(c.Dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestCache_Save_Overwrites(t *testing.T) {
	c := newTestCache(t)
	exp := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, c.Save("my-sso", Token{AccessToken: "first", ExpiresAt: exp}))
	require.NoError(t, c.Save("my-sso", Token{AccessToken: "second", ExpiresAt: exp}))

	raw, err := os.ReadFile(c.Path("my-sso"))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, "second", got["accessToken"])
}
