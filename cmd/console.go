package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/console"
	"github.com/kedwards/awst/v3/internal/creds"
	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/sso"
)

type consoleDeps struct {
	providerFactory func(ctx context.Context, profile, region string) (creds.Provider, string, error)
	signinToken     func(ctx context.Context, c console.Credentials) (string, error)
	openBrowser     func(url string) error
	openContainer   func(containerURL string) error

	// SSO device-flow collaborators for auto-login when the cached token is
	// missing or expired (same wiring as `login`).
	sessionLoader func(ctx context.Context, profile, configFile string) (sso.SSOSession, error)
	oidcFactory   func(ctx context.Context, region string) (sso.OIDCClient, error)
	cache         *sso.Cache
	sleep         func(time.Duration)
	now           func() time.Time
}

func defaultConsoleDeps() consoleDeps {
	return consoleDeps{
		providerFactory: creds.NewSDKProvider,
		signinToken: func(ctx context.Context, c console.Credentials) (string, error) {
			return console.SigninToken(ctx, http.DefaultClient, c)
		},
		openBrowser:   openBrowser,
		openContainer: launchFirefoxContainer,
		sessionLoader: sso.LoadSSOSession,
		oidcFactory: func(ctx context.Context, region string) (sso.OIDCClient, error) {
			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
			if err != nil {
				return nil, fmt.Errorf("load aws config: %w", err)
			}
			return ssooidc.NewFromConfig(cfg), nil
		},
		cache: sso.NewCache(paths.SSOCacheDir()),
		sleep: time.Sleep,
		now:   time.Now,
	}
}

func newConsoleCmd(d consoleDeps) *cobra.Command {
	var service string
	var region string
	var noBrowser bool
	var container bool
	c := &cobra.Command{
		Use:   "console [profile]",
		Short: "Open the AWS web console for a profile in a browser",
		Long: `Resolve a profile's temporary credentials, exchange them for a
console sign-in token via the AWS federation endpoint, and open the AWS web
console in your browser — the same flow as ` + "`assume -c`" + `.

With no [profile], the current environment / default credential chain is used —
so after ` + "`awst <profile>`" + ` set the env, a bare ` + "`awst console`" + `
opens that account's console. Pass [profile] to federate a specific profile.

Use --service/-s to land on a service's console home instead of the home page;
any AWS service name works (ec2, cloudwatch, s3, lambda, …).

With --container, the console opens in a per-profile Firefox container (via the
Granted Containers extension) so multiple accounts can stay logged in at once
without AWS's "you must log out" error. Each profile gets a stable, distinct
color. Container mode can also be defaulted with AWST_CONSOLE_CONTAINER=1 or
AWST_BROWSER=firefox; override the Firefox binary with AWST_FIREFOX.

Requires temporary (SSO/STS) credentials. Standard ` + "`aws`" + ` partition only.

Examples:
  awst console
  awst console dev
  awst console dev -s ec2
  awst console dev --container
  awst console dev --no-browser`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			var profile string
			if len(args) == 1 {
				profile = args[0]
			}

			// Auto-login: if a profile (arg or current AWS_PROFILE) names an
			// SSO profile, ensure a valid cached token first — running the
			// device flow when it's missing or expired, just like `awst login`.
			loginProfile := profile
			if loginProfile == "" {
				loginProfile = os.Getenv("AWS_PROFILE")
			}
			if loginProfile != "" {
				if err := d.ensureLogin(ctx, cmd, loginProfile, noBrowser); err != nil {
					return err
				}
			}

			p, effRegion, err := d.providerFactory(ctx, profile, region)
			if err != nil {
				return err
			}
			resolved, err := creds.Resolve(ctx, profile, p)
			if err != nil {
				return err
			}

			// Region for the destination URL: --region, then resolved, else us-east-1.
			effective := region
			if effective == "" {
				effective = effRegion
			}
			if effective == "" {
				effective = "us-east-1"
			}

			if resolved.SessionToken == "" {
				return fmt.Errorf("console federation requires temporary credentials; run: awst login %s", profile)
			}

			tok, err := d.signinToken(ctx, console.Credentials{
				AccessKeyID:     resolved.AccessKeyID,
				SecretAccessKey: resolved.SecretAccessKey,
				SessionToken:    resolved.SessionToken,
			})
			if err != nil {
				return err
			}

			loginURL := console.LoginURL(tok, effective, service)

			useContainer := container || containerFromEnv()
			openURL := loginURL
			if useContainer {
				name := loginProfile
				if name == "" {
					name = "aws"
				}
				openURL = console.ContainerURL(name, loginURL)
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Opening the AWS console:\n  %s\n", openURL)
			if noBrowser {
				return nil
			}
			if useContainer {
				return d.openContainer(openURL)
			}
			if d.openBrowser != nil {
				_ = d.openBrowser(loginURL)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&service, "service", "s", "", "Open this service's console home (e.g. ec2, cloudwatch, s3)")
	c.Flags().StringVarP(&region, "region", "r", "", "AWS region for the console (default: profile region, else us-east-1)")
	c.Flags().BoolVarP(&noBrowser, "no-browser", "n", false, "Print the URL only; don't try to open a browser")
	c.Flags().BoolVarP(&container, "container", "c", false, "Open in a per-profile Firefox container (Granted Containers extension)")
	return c
}

// containerFromEnv reports whether container mode is defaulted via environment.
func containerFromEnv() bool {
	return os.Getenv("AWST_CONSOLE_CONTAINER") != "" ||
		strings.EqualFold(os.Getenv("AWST_BROWSER"), "firefox")
}

// launchFirefoxContainer opens containerURL in Firefox, which the Granted
// Containers extension intercepts to open an isolated, named container tab.
func launchFirefoxContainer(containerURL string) error {
	bin, err := firefoxPath()
	if err != nil {
		return err
	}
	return exec.Command(bin, "--new-tab", containerURL).Start()
}

// firefoxPath locates the Firefox binary: AWST_FIREFOX override, then PATH,
// then OS-specific default install locations.
func firefoxPath() (string, error) {
	if p := os.Getenv("AWST_FIREFOX"); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("firefox"); err == nil {
		return p, nil
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/Applications/Firefox.app/Contents/MacOS/firefox"}
	case "windows":
		candidates = []string{`C:\Program Files\Mozilla Firefox\firefox.exe`, `C:\Program Files (x86)\Mozilla Firefox\firefox.exe`}
	default:
		candidates = []string{"/usr/bin/firefox", "/snap/bin/firefox", "/usr/local/bin/firefox"}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("firefox not found on PATH or default locations; install Firefox or set AWST_FIREFOX")
}

// ensureLogin ensures a valid SSO token for an SSO profile, running the device
// flow when needed. A profile without an sso_session (static/env creds) is a
// no-op — credential resolution handles it. Prompts go to cmd's stderr.
func (d consoleDeps) ensureLogin(ctx context.Context, cmd *cobra.Command, profile string, noBrowser bool) error {
	if d.cache == nil || d.sessionLoader == nil {
		return nil
	}
	sess, err := d.sessionLoader(ctx, profile, "")
	if err != nil {
		return nil // not an SSO profile; let credential resolution proceed
	}
	prompt := func(uri, code string) {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"Open this URL in your browser to authorize awst:\n  %s\nUser code: %s\n", uri, code)
		if !noBrowser && d.openBrowser != nil {
			_ = d.openBrowser(uri)
		}
	}
	_, _, err = sso.EnsureToken(ctx, d.cache, sess,
		func() (sso.OIDCClient, error) { return d.oidcFactory(ctx, sess.Region) },
		prompt, d.sleep, d.now)
	return err
}
