package creds

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "creds"))
}

func TestStore_CreatesDirWithMode0700(t *testing.T) {
	s := newTestStore(t)

	err := s.Save("dev", Credentials{AccessKeyID: "AKIA", SecretAccessKey: "s", SessionToken: "t"})
	require.NoError(t, err)

	info, err := os.Stat(s.Dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestStore_WritesFileWithMode0600(t *testing.T) {
	s := newTestStore(t)

	err := s.Save("dev", Credentials{AccessKeyID: "AKIA", SecretAccessKey: "s", SessionToken: "t"})
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(s.Dir, "dev.env"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestStore_OverwritesExisting(t *testing.T) {
	s := newTestStore(t)

	require.NoError(t, s.Save("dev", Credentials{AccessKeyID: "first", SecretAccessKey: "s", SessionToken: "t"}))
	require.NoError(t, s.Save("dev", Credentials{AccessKeyID: "second", SecretAccessKey: "s", SessionToken: "t"}))

	got, err := s.Load("dev")
	require.NoError(t, err)
	require.Equal(t, "second", got.AccessKeyID)
}

func TestLoad_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	want := Credentials{
		AccessKeyID:     "AKIA",
		SecretAccessKey: "secret",
		SessionToken:    "IQoJb3JpZ2luX2VjE==pad==",
		Region:          "us-east-1",
	}

	require.NoError(t, s.Save("dev", want))
	got, err := s.Load("dev")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestLoad_MissingProfile(t *testing.T) {
	s := newTestStore(t)

	_, err := s.Load("nope")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrProfileNotStored), "expected ErrProfileNotStored, got %v", err)
}

func TestList_EmptyDir(t *testing.T) {
	s := newTestStore(t)

	got, err := s.List()
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestList_ReturnsProfilesWithMtime(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Save("dev", Credentials{AccessKeyID: "A", SecretAccessKey: "B", SessionToken: "C"}))
	require.NoError(t, s.Save("prod", Credentials{AccessKeyID: "A", SecretAccessKey: "B", SessionToken: "C"}))

	// Backdate dev to test mtime is reported correctly.
	past := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(s.Dir, "dev.env"), past, past))

	got, err := s.List()
	require.NoError(t, err)
	require.Len(t, got, 2)

	byName := map[string]ProfileInfo{}
	for _, p := range got {
		byName[p.Name] = p
	}
	require.WithinDuration(t, past, byName["dev"].StoredAt, time.Second)
	require.WithinDuration(t, time.Now(), byName["prod"].StoredAt, time.Minute)
}

func TestDelete_Single(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Save("dev", Credentials{AccessKeyID: "A", SecretAccessKey: "B", SessionToken: "C"}))
	require.NoError(t, s.Save("prod", Credentials{AccessKeyID: "A", SecretAccessKey: "B", SessionToken: "C"}))

	require.NoError(t, s.Delete("dev"))

	_, err := s.Load("dev")
	require.True(t, errors.Is(err, ErrProfileNotStored))

	_, err = s.Load("prod")
	require.NoError(t, err)
}

func TestDelete_MissingProfile(t *testing.T) {
	s := newTestStore(t)

	err := s.Delete("nope")
	require.True(t, errors.Is(err, ErrProfileNotStored))
}

func TestDeleteAll(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Save("dev", Credentials{AccessKeyID: "A", SecretAccessKey: "B", SessionToken: "C"}))
	require.NoError(t, s.Save("prod", Credentials{AccessKeyID: "A", SecretAccessKey: "B", SessionToken: "C"}))

	require.NoError(t, s.DeleteAll())

	got, err := s.List()
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestList_DirDoesNotExist(t *testing.T) {
	// Mirrors awst_creds.sh:135-137: list on a missing dir is "no creds",
	// not an error.
	s := NewStore(filepath.Join(t.TempDir(), "never-created"))

	got, err := s.List()
	require.NoError(t, err)
	require.Empty(t, got)
}
