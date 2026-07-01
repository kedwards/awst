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

	"github.com/kedwards/awst/v3/internal/creds"
	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/sso"
	"github.com/kedwards/awst/v3/internal/tui"
)

type loginDeps struct {
	sessionLoader   func(ctx context.Context, profile, configFile string) (sso.SSOSession, error)
	oidcFactory     func(ctx context.Context, region string) (sso.OIDCClient, error)
	cache           *sso.Cache
	openBrowser     func(url string) error
	sleep           func(time.Duration)
	now             func() time.Time
	listProfiles    func() ([]string, error)
	selectProfile   func(items []tui.ProfileItem) (string, error)
	pickRegion      func() (string, error)
	isTerminal      func() bool
	providerFactory func(ctx context.Context, profile, region string) (creds.Provider, string, error)
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
		cache:           sso.NewCache(paths.SSOCacheDir()),
		openBrowser:     openBrowser,
		sleep:           time.Sleep,
		now:             time.Now,
		listProfiles:  defaultListProfiles,
		selectProfile: tui.SelectProfile,
		pickRegion: func() (string, error) {
			regs, err := regionsEffective()
			if err != nil {
				return "", err
			}
			return tui.SelectRegion(regs)
		},
		isTerminal:      func() bool { return term.IsTerminal(os.Stdin.Fd()) },
		providerFactory: creds.NewSDKProvider,
	}
}

func newLoginCmd(d loginDeps) *cobra.Command {
	var noBrowser bool
	var export bool
	var shellName string
	var region string
	var profileFlag string
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

With --export, after login the resolved credentials are printed as shell
export statements on stdout (status text stays on stderr), so the output can
be eval'd to set AWS_PROFILE and the credential env vars in the current shell.
This is what the ` + "`awst shell init`" + ` wrapper uses to make
` + "`awst <profile>`" + ` behave like ` + "`assume <profile>`" + `.

The profile may be given positionally or with --profile/-p; the two forms are
equivalent (giving both is an error).

Examples:
  awst login
  awst login dev
  awst login --profile dev
  awst login dev --no-browser
  eval "$(awst login dev --export)"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			profile, err := profileArg(profileFlag, args)
			if err != nil {
				return err
			}
			if profile == "" {
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

			prompt := func(uri, code string) {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Open this URL in your browser to authorize awst:\n  %s\nUser code: %s\n",
					uri, code,
				)
				if !noBrowser && d.openBrowser != nil {
					_ = d.openBrowser(uri)
				}
			}

			// Reuses a still-valid cached token; only runs the device flow when
			// the token is missing or expired.
			tok, cached, err := sso.EnsureToken(ctx, d.cache, sess,
				func() (sso.OIDCClient, error) { return d.oidcFactory(ctx, sess.Region) },
				prompt, d.sleep, d.now)
			if err != nil {
				return err
			}

			if !export {
				return nil // quiet success; the device prompt (if any) already printed
			}

			if cached {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Already logged in via sso_session %q (token valid until %s).\n",
					sess.Name, tok.ExpiresAt.Local().Format(time.RFC3339),
				)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Logged in via sso_session %q. Token cached at %s\n",
					sess.Name, d.cache.Path(sess.Name),
				)
			}

			// Resolve credentials and emit shell exports on stdout. Status text
			// above went to stderr so eval "$(...)" captures only the exports.
			shell, err := creds.ParseShell(shellName)
			if err != nil {
				return err
			}
			p, effRegion, err := d.providerFactory(ctx, profile, region)
			if err != nil {
				return err
			}
			resolved, err := creds.Resolve(ctx, profile, p)
			if err != nil {
				return err
			}
			// Prefer --region, then the profile/SDK-resolved region. When
			// neither pins a region, offer an interactive picker (skipped with
			// -r/--region set or a non-terminal stdin), and finally fall back
			// to us-east-1 so a region var is always exported.
			resolved.Region = region
			if resolved.Region == "" {
				resolved.Region = effRegion
			}
			if resolved.Region == "" && d.isTerminal() {
				r, err := d.pickRegion()
				if err != nil {
					if errors.Is(err, tui.ErrAborted) {
						return nil // user quit the region picker; nothing to do
					}
					return err
				}
				resolved.Region = r
			}
			if resolved.Region == "" {
				resolved.Region = "us-east-1"
			}
			fmt.Fprint(cmd.OutOrStdout(), creds.FormatExports(profile, resolved, shell))
			return nil
		},
	}
	c.Flags().BoolVarP(&noBrowser, "no-browser", "n", false, "Print the URL only; don't try to open a browser")
	c.Flags().BoolVarP(&export, "export", "e", false, "After login, print credential export statements on stdout for eval")
	c.Flags().StringVar(&shellName, "shell", "posix", "Export syntax with --export: posix or powershell")
	c.Flags().StringVarP(&region, "region", "r", "", "AWS region for exported credentials (default: profile region, else us-east-1)")
	c.Flags().StringVarP(&profileFlag, "profile", "p", "", "AWS profile (alternative to the positional [profile])")
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
