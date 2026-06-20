package sso

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/kedwards/awst/internal/paths"
)

// ErrNoSSOSession is returned when a profile is missing an `sso_session`
// reference. Slice 2 supports the sso_session form only.
var ErrNoSSOSession = errors.New("profile has no sso_session configured")

// SSOSession is the minimal set of fields needed to drive the OIDC device flow
// and write the SDK-compatible token cache.
type SSOSession struct {
	Name     string
	Region   string
	StartURL string
}

// LoadSSOSession reads ~/.aws/config (or configFile if non-empty) and returns
// the sso_session block referenced by the named profile.
func LoadSSOSession(ctx context.Context, profile, configFile string) (SSOSession, error) {
	if configFile == "" {
		configFile = os.Getenv("AWS_CONFIG_FILE")
	}
	var opts []func(*config.LoadSharedConfigOptions)
	if configFile != "" {
		opts = append(opts, func(o *config.LoadSharedConfigOptions) {
			o.ConfigFiles = []string{configFile}
			o.CredentialsFiles = nil
		})
	}

	shared, err := config.LoadSharedConfigProfile(ctx, profile, opts...)
	if err != nil {
		var notExist config.SharedConfigProfileNotExistError
		if errors.As(err, &notExist) {
			return SSOSession{}, profileNotFoundError(profile, configFile, err)
		}
		return SSOSession{}, fmt.Errorf("load profile %q: %w", profile, err)
	}

	if shared.SSOSession == nil {
		return SSOSession{}, fmt.Errorf("%w (profile %q; slice 2 supports sso_session form only)", ErrNoSSOSession, profile)
	}

	if shared.SSOSession.SSORegion == "" {
		return SSOSession{}, fmt.Errorf("sso-session %q missing sso_region", shared.SSOSession.Name)
	}
	if shared.SSOSession.SSOStartURL == "" {
		return SSOSession{}, fmt.Errorf("sso-session %q missing sso_start_url", shared.SSOSession.Name)
	}

	return SSOSession{
		Name:     shared.SSOSession.Name,
		Region:   shared.SSOSession.SSORegion,
		StartURL: shared.SSOSession.SSOStartURL,
	}, nil
}

// profileNotFoundError turns the SDK's terse "failed to get shared config
// profile" into something a colleague can act on: which file was searched,
// which profiles it actually contains, and the common `[name]` vs
// `[profile name]` header mistake.
func profileNotFoundError(profile, configFile string, err error) error {
	path := awsConfigPath(configFile)

	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("no AWS config file at %s — awst reads SSO profiles from there, but it doesn't exist yet"+
			"\n  set up profile %q with `aws configure sso`, or copy an existing ~/.aws/config into place",
			path, profile)
	}

	names, bare := readProfileNames(path)

	var b strings.Builder
	fmt.Fprintf(&b, "profile %q not found in %s", profile, path)
	if len(names) > 0 {
		fmt.Fprintf(&b, "\n  available profiles: %s", strings.Join(names, ", "))
	}
	if contains(bare, profile) {
		fmt.Fprintf(&b, "\n  found a [%s] section — in ~/.aws/config the header must be [profile %s], not [%s]", profile, profile, profile)
	} else {
		fmt.Fprintf(&b, "\n  hint: set it up with `aws configure sso`, or add a [profile %s] block referencing an sso_session", profile)
	}
	return fmt.Errorf("%s: %w", b.String(), err)
}

func awsConfigPath(configFile string) string {
	if configFile != "" {
		return configFile
	}
	return paths.AWSConfigFile()
}

// readProfileNames scans an AWS config file for section headers. It returns
// the usable profile names ([default] and [profile NAME]) and any bare
// [NAME] headers (which the SDK ignores in ~/.aws/config — a common mistake).
func readProfileNames(path string) (names, bare []string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
			continue
		}
		name := strings.TrimSpace(line[1 : len(line)-1])
		switch {
		case name == "default":
			names = append(names, "default")
		case strings.HasPrefix(name, "profile "):
			names = append(names, strings.TrimSpace(strings.TrimPrefix(name, "profile ")))
		case strings.HasPrefix(name, "sso-session"):
			// not a profile; ignore
		default:
			bare = append(bare, name)
		}
	}
	return names, bare
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
