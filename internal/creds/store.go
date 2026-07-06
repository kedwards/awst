package creds

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrProfileNotStored = errors.New("profile not stored")

type Store struct {
	Dir string
}

type ProfileInfo struct {
	Name     string
	StoredAt time.Time
}

func NewStore(dir string) *Store {
	return &Store{Dir: dir}
}

func (s *Store) Save(profile string, c Credentials) error {
	if err := validateProfile(profile); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("create creds dir: %w", err)
	}
	// MkdirAll respects existing perms; ensure 0700 if dir pre-existed.
	if err := os.Chmod(s.Dir, 0o700); err != nil {
		return fmt.Errorf("chmod creds dir: %w", err)
	}

	path := s.path(profile)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open creds file: %w", err)
	}
	defer f.Close()

	write := func(k, v string) error {
		if v == "" {
			return nil
		}
		_, err := fmt.Fprintf(f, "%s=%s\n", k, v)
		return err
	}
	if err := write("AWS_ACCESS_KEY_ID", c.AccessKeyID); err != nil {
		return err
	}
	if err := write("AWS_SECRET_ACCESS_KEY", c.SecretAccessKey); err != nil {
		return err
	}
	if err := write("AWS_SESSION_TOKEN", c.SessionToken); err != nil {
		return err
	}
	if err := write("AWS_REGION", c.Region); err != nil {
		return err
	}
	return nil
}

func (s *Store) Load(profile string) (Credentials, error) {
	if err := validateProfile(profile); err != nil {
		return Credentials{}, err
	}
	f, err := os.Open(s.path(profile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credentials{}, fmt.Errorf("%w: %s", ErrProfileNotStored, profile)
		}
		return Credentials{}, fmt.Errorf("open creds file: %w", err)
	}
	defer f.Close()

	var c Credentials
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Split on first '=' only — session tokens contain '='.
		i := strings.IndexByte(line, '=')
		if i < 0 {
			continue
		}
		k, v := line[:i], line[i+1:]
		switch k {
		case "AWS_ACCESS_KEY_ID":
			c.AccessKeyID = v
		case "AWS_SECRET_ACCESS_KEY":
			c.SecretAccessKey = v
		case "AWS_SESSION_TOKEN":
			c.SessionToken = v
		case "AWS_REGION":
			c.Region = v
		}
	}
	if err := scanner.Err(); err != nil {
		return Credentials{}, fmt.Errorf("read creds file: %w", err)
	}
	return c, nil
}

func (s *Store) Delete(profile string) error {
	if err := validateProfile(profile); err != nil {
		return err
	}
	err := os.Remove(s.path(profile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrProfileNotStored, profile)
		}
		return fmt.Errorf("remove creds file: %w", err)
	}
	return nil
}

func (s *Store) DeleteAll() error {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read creds dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".env") {
			continue
		}
		if err := os.Remove(filepath.Join(s.Dir, e.Name())); err != nil {
			return fmt.Errorf("remove %s: %w", e.Name(), err)
		}
	}
	return nil
}

func (s *Store) List() ([]ProfileInfo, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read creds dir: %w", err)
	}

	var out []ProfileInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".env") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", e.Name(), err)
		}
		out = append(out, ProfileInfo{
			Name:     strings.TrimSuffix(e.Name(), ".env"),
			StoredAt: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func validateProfile(name string) error {
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		return fmt.Errorf("invalid profile name %q: path separators not allowed", name)
	}
	return nil
}

func (s *Store) path(profile string) string {
	return filepath.Join(s.Dir, profile+".env")
}
