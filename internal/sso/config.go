package sso

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
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
