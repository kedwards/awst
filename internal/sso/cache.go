package sso

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Token mirrors the fields the AWS SDK Go v2 SSO token provider reads back
// from `~/.aws/sso/cache/<sha1(sessionName)>.json`. Keeping the JSON keys in
// sync with `ssocreds.tokenKnownFields` is what makes slice 1 (`awst creds
// store`) able to consume the token slice 2 writes.
type Token struct {
	AccessToken  string
	ExpiresAt    time.Time
	RefreshToken string
	ClientID     string
	ClientSecret string
}

type Cache struct {
	Dir string
}

func NewCache(dir string) *Cache { return &Cache{Dir: dir} }

// Path returns the cache filepath for sessionName. Matches the SDK's
// ssocreds.StandardCachedTokenFilepath layout when Dir is ~/.aws/sso/cache.
func (c *Cache) Path(sessionName string) string {
	sum := sha1.Sum([]byte(sessionName))
	return filepath.Join(c.Dir, strings.ToLower(hex.EncodeToString(sum[:]))+".json")
}

type tokenJSON struct {
	AccessToken  string `json:"accessToken,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
}

// Load reads and parses the cached token for sessionName. It returns an error
// if the token file is missing or cannot be parsed.
func (c *Cache) Load(sessionName string) (Token, error) {
	body, err := os.ReadFile(c.Path(sessionName))
	if err != nil {
		return Token{}, err
	}
	var raw tokenJSON
	if err := json.Unmarshal(body, &raw); err != nil {
		return Token{}, fmt.Errorf("parse sso token: %w", err)
	}
	exp, _ := time.Parse(time.RFC3339, raw.ExpiresAt) // zero time if absent/bad → treated as expired
	return Token{
		AccessToken:  raw.AccessToken,
		ExpiresAt:    exp,
		RefreshToken: raw.RefreshToken,
		ClientID:     raw.ClientID,
		ClientSecret: raw.ClientSecret,
	}, nil
}

// Delete removes the cached token for sessionName. A missing token is not an
// error (logout is idempotent).
func (c *Cache) Delete(sessionName string) error {
	if err := os.Remove(c.Path(sessionName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove sso token: %w", err)
	}
	return nil
}

// DeleteAll removes every cached SSO token (the *.json files in Dir) and
// returns how many were removed. A missing cache dir is not an error.
func (c *Cache) DeleteAll() (int, error) {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read sso cache dir: %w", err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(c.Dir, e.Name())); err != nil && !os.IsNotExist(err) {
			return n, fmt.Errorf("remove %s: %w", e.Name(), err)
		}
		n++
	}
	return n, nil
}

func (c *Cache) Save(sessionName string, t Token) error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return fmt.Errorf("create sso cache dir: %w", err)
	}
	if err := os.Chmod(c.Dir, 0o700); err != nil {
		return fmt.Errorf("chmod sso cache dir: %w", err)
	}

	payload := tokenJSON{
		AccessToken:  t.AccessToken,
		ExpiresAt:    t.ExpiresAt.UTC().Format(time.RFC3339),
		RefreshToken: t.RefreshToken,
		ClientID:     t.ClientID,
		ClientSecret: t.ClientSecret,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal sso token: %w", err)
	}

	final := c.Path(sessionName)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write sso token tmp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("rename sso token: %w", err)
	}
	return nil
}
