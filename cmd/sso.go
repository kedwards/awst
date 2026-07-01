package cmd

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	awssso "github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/sso"
)

type ssoDeps struct {
	cache         *sso.Cache
	oidcFactory   func(ctx context.Context, region string) (sso.OIDCClient, error)
	portalFactory func(ctx context.Context, region string) (sso.Portal, error)
	openBrowser   func(url string) error
	sleep         func(time.Duration)
	now           func() time.Time
	configPath    func() string
}

func defaultSSODeps() ssoDeps {
	return ssoDeps{
		cache: sso.NewCache(paths.SSOCacheDir()),
		oidcFactory: func(ctx context.Context, region string) (sso.OIDCClient, error) {
			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
			if err != nil {
				return nil, fmt.Errorf("load aws config: %w", err)
			}
			return ssooidc.NewFromConfig(cfg), nil
		},
		portalFactory: func(ctx context.Context, region string) (sso.Portal, error) {
			// The portal APIs authorize with the SSO access token passed per
			// request, so no credential resolution is needed here.
			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
			if err != nil {
				return nil, fmt.Errorf("load aws config: %w", err)
			}
			return awssso.NewFromConfig(cfg), nil
		},
		openBrowser: openBrowser,
		sleep:       time.Sleep,
		now:         time.Now,
		configPath:  paths.AWSConfigFile,
	}
}

func newSSOCmd(d ssoDeps) *cobra.Command {
	c := &cobra.Command{
		Use:   "sso",
		Short: "Manage AWS SSO (IAM Identity Center) configuration",
	}
	c.AddCommand(newSSOConfigureCmd(d))
	return c
}

func newSSOConfigureCmd(d ssoDeps) *cobra.Command {
	var (
		startURL  string
		ssoRegion string
		session   string
		region    string
		naming    string
		noBrowser bool
	)
	c := &cobra.Command{
		Use:   "configure",
		Short: "Generate ~/.aws/config profiles from an SSO start URL",
		Long: `Log in to IAM Identity Center and write an [sso-session] block plus a
[profile] block for every account/role the SSO session grants, merging into
~/.aws/config (backed up to ~/.aws/config.bak) without touching other entries.

A still-valid cached token is reused; otherwise the device-authorization flow
runs first. Re-running upserts the same profiles (it never deletes ones removed
upstream).

Examples:
  awst sso configure --start-url https://my-org.awsapps.com/start --sso-region us-east-1
  awst sso configure --start-url https://my-org.awsapps.com/start --sso-region us-east-1 --naming accountid-role`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if startURL == "" || ssoRegion == "" {
				return fmt.Errorf("--start-url and --sso-region are required")
			}
			if u, err := url.Parse(startURL); err != nil || !strings.HasSuffix(u.Hostname(), ".awsapps.com") {
				return fmt.Errorf("--start-url must be an awsapps.com endpoint, got %q", startURL)
			}
			if !slices.Contains(sso.NamingSchemes, naming) {
				return fmt.Errorf("invalid --naming %q (want one of %s)", naming, strings.Join(sso.NamingSchemes, ", "))
			}
			if session == "" {
				session = deriveSessionName(startURL)
			}
			if region == "" {
				region = ssoRegion
			}

			sess := sso.SSOSession{Name: session, Region: ssoRegion, StartURL: startURL}

			prompt := func(uri, code string) {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Open this URL in your browser to authorize awst:\n  %s\nUser code: %s\n",
					uri, code,
				)
				if !noBrowser && d.openBrowser != nil {
					_ = d.openBrowser(uri)
				}
			}

			tok, cached, err := sso.EnsureToken(ctx, d.cache, sess,
				func() (sso.OIDCClient, error) { return d.oidcFactory(ctx, ssoRegion) },
				prompt, d.sleep, d.now)
			if err != nil {
				return err
			}
			if !cached {
				fmt.Fprintf(cmd.ErrOrStderr(), "Logged in via sso_session %q.\n", sess.Name)
			}

			portal, err := d.portalFactory(ctx, ssoRegion)
			if err != nil {
				return err
			}
			roles, err := sso.ListAccountRoles(ctx, portal, tok.AccessToken)
			if err != nil {
				return err
			}
			if len(roles) == 0 {
				return fmt.Errorf("no accounts or roles are accessible with this SSO session")
			}

			rolesPerAccount := map[string]int{}
			for _, r := range roles {
				rolesPerAccount[r.AccountID]++
			}

			profiles := make([]sso.NamedProfile, 0, len(roles))
			for _, r := range roles {
				name, err := sso.ProfileName(naming, r, rolesPerAccount[r.AccountID] > 1)
				if err != nil {
					return err
				}
				profiles = append(profiles, sso.NamedProfile{Name: name, AccountID: r.AccountID, RoleName: r.RoleName})
			}

			path := d.configPath()
			if err := sso.WriteConfig(path, sess, region, profiles); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(),
				"Wrote %d profiles for %d accounts to %s (backup at %s.bak)\n",
				len(profiles), len(rolesPerAccount), path, path,
			)
			return nil
		},
	}
	c.Flags().StringVar(&startURL, "start-url", "", "SSO start URL, e.g. https://my-org.awsapps.com/start (required)")
	c.Flags().StringVar(&ssoRegion, "sso-region", "", "Region of the Identity Center instance (required)")
	c.Flags().StringVar(&session, "session", "", "sso_session block name (default: derived from the start URL host)")
	c.Flags().StringVar(&region, "region", "", "Default region written on each profile (default: --sso-region)")
	c.Flags().StringVar(&naming, "naming", sso.NameAccountRole, "Profile naming scheme: "+strings.Join(sso.NamingSchemes, " | "))
	c.Flags().BoolVarP(&noBrowser, "no-browser", "n", false, "Print the URL only; don't try to open a browser")
	return c
}

// deriveSessionName uses the first label of the start URL's host (e.g.
// "my-org" from https://my-org.awsapps.com/start), falling back to "sso".
func deriveSessionName(startURL string) string {
	u, err := url.Parse(startURL)
	if err != nil || u.Host == "" {
		return "sso"
	}
	if label, _, found := strings.Cut(u.Hostname(), "."); found && label != "" {
		return label
	}
	return "sso"
}
