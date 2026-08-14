package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/creds"
	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/sso"
)

type logoutDeps struct {
	sessionLoader func(ctx context.Context, profile, configFile string) (sso.SSOSession, error)
	cache         *sso.Cache
	// stderrIsTerminal gates the shell-integration advice so it stays out of
	// piped output and CI logs; getenv reads the wrapper's marker.
	stderrIsTerminal func() bool
	getenv           func(string) string
}

func defaultLogoutDeps() logoutDeps {
	return logoutDeps{
		sessionLoader:    sso.LoadSSOSession,
		cache:            sso.NewCache(paths.SSOCacheDir()),
		stderrIsTerminal: func() bool { return term.IsTerminal(os.Stderr.Fd()) },
		getenv:           os.Getenv,
	}
}

func newLogoutCmd(d logoutDeps) *cobra.Command {
	var profileFlag string
	var export bool
	var shellName string
	var clearCache bool
	c := &cobra.Command{
		Use:   "logout [profile]",
		Short: "Clear the AWS credential env vars from the current shell",
		Long: `Clear the AWS credential env vars from the current shell (the counterpart
to ` + "`awst <profile>`" + `). By default the cached SSO token is left in place,
so the next login skips the device flow.

With --clear-cache, the cached SSO token is also removed (like
` + "`aws sso logout`" + `), so the next login re-runs the device flow. With a
[profile], only that profile's sso_session token is cleared; with no [profile],
all cached SSO tokens are cleared.

Clearing the shell env vars requires the ` + "`awst shell init`" + ` wrapper, which
runs ` + "`logout --export`" + ` and eval's the emitted unset statements. --export
prints those statements on stdout (status text stays on stderr) directly.

The profile may be given positionally or with --profile/-p; the two forms are
equivalent (giving both is an error).

Examples:
  awst logout                      # clear shell creds, keep the SSO token
  awst logout --clear-cache        # also forget all cached SSO tokens
  awst logout --clear-cache dev    # also forget only dev's sso_session token
  eval "$(awst logout --export)"`,
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

			if clearCache {
				if profile == "" {
					n, err := d.cache.DeleteAll()
					if err != nil {
						return err
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Cleared %d cached SSO token(s). Next login will re-run the device flow.\n", n)
				} else {
					sess, err := d.sessionLoader(ctx, profile, "")
					if err != nil {
						return err
					}
					if err := d.cache.Delete(sess.Name); err != nil {
						return err
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Cleared SSO token for sso_session %q. Next login will re-run the device flow.\n", sess.Name)
				}
			}

			if export {
				shell, err := creds.ParseShell(shellName)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), creds.FormatUnset(shell))
				fmt.Fprintln(cmd.ErrOrStderr(), "Cleared AWS credential env vars from the current shell.")
			} else {
				// Without --export there is nothing for the shell to eval, so
				// the env vars survive. Say so rather than exiting quietly.
				d.reportNoExport(cmd)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&profileFlag, "profile", "p", "", "AWS profile (alternative to the positional [profile])")
	c.Flags().BoolVarP(&export, "export", "e", false, "Print statements on stdout that unset the AWS credential env vars, for eval")
	c.Flags().StringVar(&shellName, "shell", "posix", "Unset syntax with --export: posix, fish, or powershell")
	c.Flags().BoolVar(&clearCache, "clear-cache", false, "Also remove the cached SSO token(s), forcing a device-flow re-login")
	return c
}

// reportNoExport explains why a logout without --export left the AWS
// credential env vars in place: clearing them takes an eval in the caller's
// shell, which only the `awst shell init` wrapper performs.
func (d logoutDeps) reportNoExport(cmd *cobra.Command) {
	w := cmd.ErrOrStderr()
	fmt.Fprintln(w, "No credential env vars were cleared: without --export there is nothing for the shell to eval.")

	si := detectShellIntegration(d.getenv)
	if si.loaded {
		if si.stale() {
			fmt.Fprintf(w, "note: shell integration loaded from awst %s but this is awst %s; run `awst shell install --force` and restart your shell.\n",
				si.version, version,
			)
		}
		return
	}
	if !shellTTY(d.stderrIsTerminal) {
		return
	}
	fmt.Fprintf(w, "\nThe awst shell integration is not loaded, so `awst logout` cannot clear them for you.\n")
	shellSetupHint(w, logoutShellSetupHint(d.getenv))
}
