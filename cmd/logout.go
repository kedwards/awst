package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

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
	c := &cobra.Command{
		Use:   "logout [profile]",
		Short: "Clear cached SSO session token(s)",
		Long: `Remove cached SSO session tokens so the next login runs the device
flow again.

With a [profile], only that profile's sso_session token is cleared. With no
[profile], all cached SSO tokens are cleared (like ` + "`aws sso logout`" + `).

Examples:
  awst logout          # clear all cached SSO tokens
  awst logout dev      # clear only dev's sso_session token`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if len(args) == 0 {
				n, err := d.cache.DeleteAll()
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Cleared %d cached SSO token(s). Next login will re-run the device flow.\n", n)
				return nil
			}

			profile := args[0]
			sess, err := d.sessionLoader(ctx, profile, "")
			if err != nil {
				return err
			}
			if err := d.cache.Delete(sess.Name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Cleared SSO token for sso_session %q. Next login will re-run the device flow.\n", sess.Name)
			return nil
		},
	}
	return c
}
