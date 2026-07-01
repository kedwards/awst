package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/creds"
	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/sso"
)

type logoutDeps struct {
	sessionLoader func(ctx context.Context, profile, configFile string) (sso.SSOSession, error)
	cache         *sso.Cache
}

func defaultLogoutDeps() logoutDeps {
	return logoutDeps{
		sessionLoader: sso.LoadSSOSession,
		cache:         sso.NewCache(paths.SSOCacheDir()),
	}
}

func newLogoutCmd(d logoutDeps) *cobra.Command {
	var profileFlag string
	var export bool
	var shellName string
	var keepToken bool
	c := &cobra.Command{
		Use:   "logout [profile]",
		Short: "Clear cached SSO session token(s)",
		Long: `Remove cached SSO session tokens so the next login runs the device
flow again.

With a [profile], only that profile's sso_session token is cleared. With no
[profile], all cached SSO tokens are cleared (like ` + "`aws sso logout`" + `).

With --export, statements that unset the AWS credential env vars are printed
on stdout (status text stays on stderr), so the output can be eval'd to clear
them from the current shell. This is what the ` + "`awst shell init`" + ` wrapper
uses so ` + "`awst logout`" + ` also drops the credentials from your shell.

With --keep-token, the cached SSO token is left in place and only the shell
env vars are cleared, so the next login skips the device flow.

The profile may be given positionally or with --profile/-p; the two forms are
equivalent (giving both is an error).

Examples:
  awst logout              # clear all cached SSO tokens
  awst logout dev          # clear only dev's sso_session token
  awst logout --keep-token # drop creds from the shell, keep the SSO token
  awst logout --profile dev
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

			switch {
			case keepToken:
				// Drop the creds from the shell but keep the cached SSO token,
				// so the next login skips the device flow.
				fmt.Fprintln(cmd.ErrOrStderr(), "Cleared AWS credential env vars; cached SSO token kept.")
			case profile == "":
				n, err := d.cache.DeleteAll()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Cleared %d cached SSO token(s). Next login will re-run the device flow.\n", n)
			default:
				sess, err := d.sessionLoader(ctx, profile, "")
				if err != nil {
					return err
				}
				if err := d.cache.Delete(sess.Name); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Cleared SSO token for sso_session %q. Next login will re-run the device flow.\n", sess.Name)
			}

			if export {
				shell, err := creds.ParseShell(shellName)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), creds.FormatUnset(shell))
			}
			return nil
		},
	}
	c.Flags().StringVarP(&profileFlag, "profile", "p", "", "AWS profile (alternative to the positional [profile])")
	c.Flags().BoolVarP(&export, "export", "e", false, "Also print statements on stdout that unset the AWS credential env vars, for eval")
	c.Flags().StringVar(&shellName, "shell", "posix", "Unset syntax with --export: posix or powershell")
	c.Flags().BoolVar(&keepToken, "keep-token", false, "Clear the shell env vars only; keep the cached SSO token so the next login skips the device flow")
	return c
}
