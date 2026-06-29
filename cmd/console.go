package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/kedwards/awst/v3/internal/tui"
)

type consoleDeps struct {
	providerFactory func(ctx context.Context, profile, region string) (creds.Provider, string, error)
	signinToken     func(ctx context.Context, c console.Credentials) (string, error)
	openBrowser     func(url string) error
	openFirefox     func(url string) error
	detectContainer func() bool

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
		openBrowser:     openBrowser,
		openFirefox:     launchFirefoxTab,
		detectContainer: console.ContainerExtensionInstalled,
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
	var noContainer bool
	var installExt bool
	var profileFlag string
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

If the awst Containers Firefox extension is detected, the console opens by
default in a per-profile Firefox container so multiple accounts can stay logged
in at once without AWS's "you must log out" error (each profile gets a stable,
distinct color). If it isn't detected, a regular Firefox tab is opened instead.
Use --container to force a container (skipping detection) or --no-container to
force a plain tab; AWST_CONSOLE_CONTAINER=1 / AWST_BROWSER=firefox also force
container mode. Override the Firefox binary with AWST_FIREFOX.

Run --install-extension to install the awst Containers extension and exit.

Requires temporary (SSO/STS) credentials. Standard ` + "`aws`" + ` partition only.

The profile may be given positionally or with --profile/-p; the two forms are
equivalent (giving both is an error).

Examples:
  awst console
  awst console dev
  awst console --profile dev
  awst console dev -s ec2
  awst console dev --container
  awst console dev --no-browser`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if installExt {
				return installExtension(cmd.OutOrStdout())
			}

			profile, err := profileArg(profileFlag, args)
			if err != nil {
				return err
			}

			// Resolve profile/region, prompting with a picker when missing and
			// interactive (skips the region prompt when already resolvable).
			profile, region, err = resolveProfileRegion(ctx, profile, region, isStdinTerminal)
			if err != nil {
				if errors.Is(err, tui.ErrAborted) {
					return nil
				}
				return err
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

			// Decide between a container tab and a plain Firefox tab.
			// --no-container forces plain; --container (or the env hints) forces
			// container and skips detection; otherwise auto-detect the extension.
			useContainer := false
			switch {
			case noContainer:
				useContainer = false
			case container || containerFromEnv():
				useContainer = true
			default:
				useContainer = d.detectContainer != nil && d.detectContainer()
			}

			openURL := loginURL
			if useContainer {
				name := loginProfile
				if name == "" {
					name = "aws"
				}
				openURL = console.ContainerURL(name, loginURL)
			}

			if noBrowser {
				// Printing is the whole point here: emit the bare URL on stdout
				// so it can be piped/copied. Otherwise stay quiet and just open.
				fmt.Fprintln(cmd.OutOrStdout(), openURL)
				return nil
			}

			// Both modes open Firefox; only the URL differs. In plain mode, if
			// Firefox isn't available, fall back to the system default browser
			// so the console still opens somewhere.
			if err := d.openFirefox(openURL); err != nil {
				if useContainer {
					return err
				}
				if d.openBrowser != nil {
					_ = d.openBrowser(loginURL)
				}
			}
			return nil
		},
	}
	c.Flags().StringVarP(&service, "service", "s", "", "Open this service's console home (e.g. ec2, cloudwatch, s3)")
	c.Flags().StringVarP(&region, "region", "r", "", "AWS region for the console (default: profile region, else us-east-1)")
	c.Flags().BoolVarP(&noBrowser, "no-browser", "n", false, "Print the URL only; don't try to open a browser")
	c.Flags().BoolVarP(&container, "container", "c", false, "Force a per-profile Firefox container, skipping extension detection")
	c.Flags().BoolVar(&noContainer, "no-container", false, "Force a plain Firefox tab even if the awst Containers extension is detected")
	c.Flags().BoolVar(&installExt, "install-extension", false, "Install the awst Containers Firefox extension and exit")
	c.Flags().StringVarP(&profileFlag, "profile", "p", "", "AWS profile (alternative to the positional [profile])")
	return c
}

// containerFromEnv reports whether container mode is defaulted via environment.
func containerFromEnv() bool {
	return os.Getenv("AWST_CONSOLE_CONTAINER") != "" ||
		strings.EqualFold(os.Getenv("AWST_BROWSER"), "firefox")
}

// launchFirefoxTab opens url in a new Firefox tab. For a container URL
// (ext+awst-containers:...) the awst Containers extension intercepts it and
// opens an isolated, named container tab; a plain federation URL opens as an
// ordinary tab.
func launchFirefoxTab(url string) error {
	bin, err := firefoxPath()
	if err != nil {
		return err
	}
	return exec.Command(bin, "--new-tab", url).Start()
}

// installExtension installs the awst Containers Firefox extension. If a signed
// XPI path is given via AWST_EXTENSION_XPI, it opens it with Firefox to trigger
// the (user-level, no-admin) install prompt; otherwise it prints instructions.
func installExtension(out io.Writer) error {
	if xpi := os.Getenv("AWST_EXTENSION_XPI"); xpi != "" {
		if _, err := os.Stat(xpi); err == nil {
			bin, err := firefoxPath()
			if err != nil {
				return err
			}
			if err := exec.Command(bin, xpi).Start(); err != nil {
				return err
			}
			fmt.Fprintf(out, "Opening Firefox to install the awst Containers extension from %s …\nAccept the prompt to finish.\n", xpi)
			return nil
		}
		fmt.Fprintf(out, "AWST_EXTENSION_XPI=%s not found; falling back to instructions.\n\n", xpi)
	}
	fmt.Fprint(out, installInstructions)
	return nil
}

const installInstructions = `The awst Containers Firefox extension opens each AWS profile in its own
isolated container so multiple accounts stay logged in at once.

Install (signed release):
  1. Download awst-containers.xpi from
     https://github.com/kedwards/awst/releases/latest
  2. Open it in Firefox (File ▸ Open File…, or drag it onto a window) and
     accept the install prompt.
     Or set AWST_EXTENSION_XPI=/path/to/awst-containers.xpi and rerun
     'awst console --install-extension' to open it automatically.

Develop / load unsigned (Firefox Developer Edition or Nightly):
  about:debugging ▸ This Firefox ▸ Load Temporary Add-on… ▸ pick
  extension/manifest.json from the awst source tree.
`

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
