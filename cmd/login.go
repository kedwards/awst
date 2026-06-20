package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/spf13/cobra"

	"github.com/kedwards/aws-tools/internal/paths"
	"github.com/kedwards/aws-tools/internal/sso"
)

type loginDeps struct {
	sessionLoader func(ctx context.Context, profile, configFile string) (sso.SSOSession, error)
	oidcFactory   func(ctx context.Context, region string) (sso.OIDCClient, error)
	cache         *sso.Cache
	openBrowser   func(url string) error
	sleep         func(time.Duration)
	now           func() time.Time
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
		cache:       sso.NewCache(paths.SSOCacheDir()),
		openBrowser: openBrowser,
		sleep:       time.Sleep,
		now:         time.Now,
	}
}

func newLoginCmd(d loginDeps) *cobra.Command {
	var noBrowser bool
	c := &cobra.Command{
		Use:   "login <profile>",
		Short: "Log in via SSO device flow for <profile>",
		Long: `Run the IAM Identity Center device-authorization flow for <profile>'s
sso_session and cache the resulting token at the SDK-standard path
(~/.aws/sso/cache/<sha1(session)>.json).

After a successful login, the AWS SDK default credential chain (used by
` + "`awst creds store`" + `) will resolve credentials for any profile that
references the same sso_session.

Examples:
  awst login dev
  awst login dev --no-browser`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := args[0]
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			sess, err := d.sessionLoader(ctx, profile, "")
			if err != nil {
				return err
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
