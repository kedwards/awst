package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/sso"
	"github.com/kedwards/awst/v3/internal/tui"
)

type loginDeps struct {
	sessionLoader func(ctx context.Context, profile, configFile string) (sso.SSOSession, error)
	oidcFactory   func(ctx context.Context, region string) (sso.OIDCClient, error)
	cache         *sso.Cache
	openBrowser   func(url string) error
	sleep         func(time.Duration)
	now           func() time.Time
	listProfiles  func() ([]string, error)
	selectProfile func(items []tui.ProfileItem) (string, error)
	isTerminal    func() bool
}

func defaultLoginDeps() loginDeps {
	return loginDeps{
		sessionLoader: sso.LoadSSOSession,
		oidcFactory: func(ctx context.Context, region string) (sso.OIDCClient, error) {
			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
			if err != nil {
				return nil, fmt.Errorf("load aws config: %w", err)
			}
			return ssooidc.NewFromConfig(cfg), nil
		},
		cache:         sso.NewCache(paths.SSOCacheDir()),
		openBrowser:   openBrowser,
		sleep:         time.Sleep,
		now:           time.Now,
		listProfiles:  defaultListProfiles,
		selectProfile: tui.SelectProfile,
		isTerminal:    func() bool { return term.IsTerminal(os.Stdin.Fd()) },
	}
}

func newLoginCmd(d loginDeps) *cobra.Command {
	var noBrowser bool
	c := &cobra.Command{
		Use:   "login [profile]",
		Short: "Log in via SSO device flow",
		Long: `Run the IAM Identity Center device-authorization flow for a profile's
sso_session and cache the resulting token at the SDK-standard path
(~/.aws/sso/cache/<sha1(session)>.json).

With no [profile], an interactive picker lists the SSO-capable profiles from
~/.aws/config to choose from. Pass [profile] to skip the picker.

After a successful login, the AWS SDK default credential chain (used by
` + "`awst creds store`" + `) will resolve credentials for any profile that
references the same sso_session.

Examples:
  awst login
  awst login dev
  awst login dev --no-browser`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			var profile string
			if len(args) == 1 {
				profile = args[0]
			} else {
				p, err := d.pickProfile(ctx, cmd)
				if err != nil {
					if errors.Is(err, tui.ErrAborted) {
						return nil // user quit the picker; nothing to do
					}
					return err
				}
				profile = p
			}

			sess, err := d.sessionLoader(ctx, profile, "")
			if err != nil {
				return err
			}

			// Skip the SSO device flow if a still-valid token is already cached.
			if tok, err := d.cache.Load(sess.Name); err == nil && tok.AccessToken != "" && tok.ExpiresAt.After(d.now()) {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Already logged in via sso_session %q (token valid until %s).\n",
					sess.Name, tok.ExpiresAt.Local().Format(time.RFC3339),
				)
				return nil
			}

			oidc, err := d.oidcFactory(ctx, sess.Region)
			if err != nil {
				return err
			}

			prompt := func(uri, code string) {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Open this URL in your browser to authorize awst:\n  %s\nUser code: %s\n",
					uri, code,
				)
				if !noBrowser && d.openBrowser != nil {
					_ = d.openBrowser(uri)
				}
			}

			tok, err := sso.Login(ctx, oidc, sess, prompt, d.sleep, d.now)
			if err != nil {
				return err
			}

			if err := d.cache.Save(sess.Name, tok); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(),
				"Logged in via sso_session %q. Token cached at %s\n",
				sess.Name, d.cache.Path(sess.Name),
			)
			return nil
		},
	}
	c.Flags().BoolVarP(&noBrowser, "no-browser", "n", false, "Print the URL only; don't try to open a browser")
	return c
}

// pickProfile presents an interactive picker of the SSO-capable profiles in
// ~/.aws/config and returns the chosen profile name. It requires an
// interactive terminal; in a pipe or CI it returns an error telling the user
// to pass the profile explicitly. Returns tui.ErrAborted if the user quits.
func (d loginDeps) pickProfile(ctx context.Context, cmd *cobra.Command) (string, error) {
	if !d.isTerminal() {
		return "", fmt.Errorf("no profile given and stdin is not a terminal; pass one explicitly: awst login <profile>")
	}

	names, err := d.listProfiles()
	if err != nil {
		return "", err
	}

	// A profile is SSO-capable iff its sso_session resolves.
	var items []tui.ProfileItem
	for _, name := range names {
		sess, err := d.sessionLoader(ctx, name, "")
		if err != nil {
			continue
		}
		items = append(items, tui.ProfileItem{Profile: name, Session: sess.Name})
	}

	switch len(items) {
	case 0:
		return "", fmt.Errorf("no SSO-capable profiles found in %s; add a profile with an sso_session", paths.AWSConfigFile())
	case 1:
		fmt.Fprintf(cmd.ErrOrStderr(), "Using the only SSO profile: %s\n", items[0].Profile)
		return items[0].Profile, nil
	default:
		return d.selectProfile(items)
	}
}

// openBrowser launches the system default URL handler. Failures are silently
// ignored by callers because the URL is already printed for manual entry.
func openBrowser(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		c = exec.Command("xdg-open", url)
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	return c.Start()
}
