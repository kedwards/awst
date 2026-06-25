package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/kedwards/awst/v3/internal/connect"
	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/ssmexec"
	"github.com/kedwards/awst/v3/internal/sso"
	"github.com/kedwards/awst/v3/internal/tui"
)

type ssmClients struct {
	SSM        connect.SSMClient
	EC2        connect.EC2Client
	SSMSession connect.SSMSessionClient
	Cmd        ssmexec.CmdClient
	Region     string
	Profile    string
}

type connectDeps struct {
	clients        func(ctx context.Context, profile, region string) (*ssmClients, error)
	runner         connect.PluginRunner
	lookPlugin     func() error
	connFile       string // default connections file; -f overrides at runtime
	selectInstance func(items []tui.InstanceItem) (string, error)
	isTerminal     func() bool

	// SSO device-flow collaborators for auto-login when the cached token is
	// missing or expired (same wiring as `login` and `console`).
	sessionLoader func(ctx context.Context, profile, configFile string) (sso.SSOSession, error)
	oidcFactory   func(ctx context.Context, region string) (sso.OIDCClient, error)
	cache         *sso.Cache
	openBrowser   func(url string) error
	sleep         func(time.Duration)
	now           func() time.Time
}

var awsProfileEnvMu sync.Mutex

func defaultConnectDeps() connectDeps {
	pluginBin := connect.PluginName
	if v := os.Getenv("AWST_SSM_PLUGIN"); v != "" {
		pluginBin = v
	}
	connFile := os.Getenv("AWST_CONN_FILE")
	if connFile == "" {
		connFile = paths.ConnectionsFile()
	}
	return connectDeps{
		connFile: connFile,
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
		clients: func(ctx context.Context, profile, region string) (*ssmClients, error) {
			cfg, err := loadAWSConfig(ctx, profile, region)
			if err != nil {
				return nil, fmt.Errorf("load aws config: %w", err)
			}
			ssmClient := ssm.NewFromConfig(cfg)
			return &ssmClients{
				SSM:        ssmClient,
				EC2:        ec2.NewFromConfig(cfg),
				SSMSession: ssmClient,
				Cmd:        ssmClient,
				Region:     cfg.Region,
				Profile:    profile,
			}, nil
		},
		runner: connect.ExecRunner{Binary: pluginBin},
		lookPlugin: func() error {
			_, err := exec.LookPath(pluginBin)
			return err
		},
		selectInstance: tui.SelectInstance,
		isTerminal:     func() bool { return term.IsTerminal(os.Stdin.Fd()) },
	}
}

func loadAWSConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	cfg, err := config.LoadDefaultConfig(ctx, awsConfigLoadOptions(profile, region)...)
	if err == nil {
		return cfg, nil
	}
	if profile != "" || !shouldRetryWithAmbientEnvCreds(err) {
		return aws.Config{}, err
	}
	return loadAWSConfigIgnoringProfileEnv(ctx, region)
}

func awsConfigLoadOptions(profile, region string) []func(*config.LoadOptions) error {
	opts := []func(*config.LoadOptions) error{}
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	return opts
}

func shouldRetryWithAmbientEnvCreds(err error) bool {
	var missing config.SharedConfigProfileNotExistError
	if !errors.As(err, &missing) {
		return false
	}
	return os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != ""
}

func loadAWSConfigIgnoringProfileEnv(ctx context.Context, region string) (aws.Config, error) {
	awsProfileEnvMu.Lock()
	defer awsProfileEnvMu.Unlock()

	saved := map[string]*string{}
	for _, key := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE"} {
		if v, ok := os.LookupEnv(key); ok {
			v := v
			saved[key] = &v
			if err := os.Unsetenv(key); err != nil {
				return aws.Config{}, err
			}
			continue
		}
		saved[key] = nil
	}
	defer func() {
		for key, value := range saved {
			if value == nil {
				_ = os.Unsetenv(key)
				continue
			}
			_ = os.Setenv(key, *value)
		}
	}()

	return config.LoadDefaultConfig(ctx, awsConfigLoadOptions("", region)...)
}

// ensureLogin ensures a valid SSO token for an SSO profile, running the device
// flow when needed. A profile without an sso_session (static/env creds) is a
// no-op — credential resolution handles it. Prompts go to cmd's stderr.
func (d connectDeps) ensureLogin(ctx context.Context, cmd *cobra.Command, profile string) error {
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
		if d.openBrowser != nil {
			_ = d.openBrowser(uri)
		}
	}
	_, _, err = sso.EnsureToken(ctx, d.cache, sess,
		func() (sso.OIDCClient, error) { return d.oidcFactory(ctx, sess.Region) },
		prompt, d.sleep, d.now)
	return err
}

func newConnectCmd(d connectDeps) *cobra.Command {
	var profile, region, forwardSpec, host, file string
	c := &cobra.Command{
		Use:   "connect [instance|connection]",
		Short: "Open an SSM shell session or port-forward to an EC2 instance",
		Long: `Open an SSM session on an SSM-managed EC2 instance.

Default (shell): if the argument starts with "i-" it's an instance ID,
otherwise it's a case-insensitive substring match on the Name tag.

Port-forward (--forward): tunnel one or more local ports to the instance,
or to a host reachable from it via --host (e.g. an RDS endpoint). The
spec is a comma-separated list of PORT or LOCAL:REMOTE mappings.

Saved connection: if the argument matches a [section] in the connections
file (default ~/.config/aws-tools/connections.config, override with -f or
AWST_CONN_FILE) a port-forward starts using that section's settings.

Requires session-manager-plugin on PATH (override with AWST_SSM_PLUGIN).

Examples:
  awst connect i-0123abc
  awst connect web-prod
  awst connect web-prod --forward 5432:5432
  awst connect web --forward 8428,9093 --host mon.internal
  awst connect Engine            # named saved connection
  awst connect                   # lists available instances`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := d.lookPlugin(); err != nil {
				return fmt.Errorf("session-manager-plugin not found on PATH (override with AWST_SSM_PLUGIN): %w", err)
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}

			// Validate the ad-hoc spec up front, before any AWS calls.
			var adhoc []connect.PortForward
			if forwardSpec != "" {
				var err error
				if adhoc, err = connect.ParseForwardSpec(forwardSpec, host); err != nil {
					return err
				}
			}

			// A bare arg may name a saved connection (only when not already
			// an explicit --forward). Missing file just means "no saved
			// connections" — fall through to instance handling.
			var conn *connect.Connection
			if forwardSpec == "" && arg != "" {
				connFile := d.connFile
				if file != "" {
					connFile = file
				}
				if c, ok, err := lookupConnection(connFile, arg); err != nil {
					return err
				} else if ok {
					conn = &c
				}
			}

			// A saved connection can pin profile/region; explicit flags win.
			effProfile, effRegion := profile, region
			if conn != nil {
				if effProfile == "" {
					effProfile = conn.Profile
				}
				if effRegion == "" {
					effRegion = conn.Region
				}
			}

			// Resolve profile/region, prompting with a picker when missing and
			// interactive (skips the region prompt when already resolvable).
			var perr error
			effProfile, effRegion, perr = resolveProfileRegion(ctx, effProfile, effRegion, d.isTerminal)
			if perr != nil {
				if errors.Is(perr, tui.ErrAborted) {
					return nil
				}
				return perr
			}

			// Auto-login: if we have a profile (flag, saved connection, or
			// AWS_PROFILE), ensure a valid SSO token first for SSO profiles.
			loginProfile := effProfile
			if loginProfile == "" {
				loginProfile = os.Getenv("AWS_PROFILE")
			}
			if loginProfile != "" {
				if err := d.ensureLogin(ctx, cmd, loginProfile); err != nil {
					return err
				}
			}

			clients, err := d.clients(ctx, effProfile, effRegion)
			if err != nil {
				return err
			}
			list, err := connect.List(ctx, clients.SSM, clients.EC2)
			if err != nil {
				return authHint(err, clients.Profile)
			}

			// For a saved connection the instance filter is its name= field.
			filter := arg
			if conn != nil {
				filter = conn.Label
			}
			matches := connect.Resolve(filter, list)

			forwarding := forwardSpec != "" || conn != nil

			var inst connect.Instance
			switch {
			case len(matches) == 0 && filter == "":
				printInstances(cmd.OutOrStdout(), list)
				return errors.New("no SSM-managed instances found in this account/region")
			case len(matches) == 0:
				printInstances(cmd.OutOrStdout(), list)
				return fmt.Errorf("no instance matched %q", filter)
			case len(matches) == 1:
				inst = matches[0]
			default: // 2+ candidates — pick interactively, or error in a pipe/CI
				if !d.isTerminal() {
					printInstances(cmd.OutOrStdout(), matches)
					return fmt.Errorf("ambiguous: %d instances matched %q (refine the pattern, pass an i-… id, or run interactively)", len(matches), filter)
				}
				id, err := d.selectInstance(toInstanceItems(matches))
				if err != nil {
					if errors.Is(err, tui.ErrAborted) {
						return nil // user quit the picker; nothing to do
					}
					return err
				}
				inst = instByID(matches, id)
			}

			endpoint := connect.SSMEndpoint(clients.Region)

			if forwarding {
				pfs := adhoc
				if conn != nil {
					pfs = conn.Forwards
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Port-forwarding to %s (%s) in %s...\n", inst.Name, inst.ID, clients.Region)
				return authHint(runForwards(ctx, clients, d.runner, pfs, inst.ID, endpoint, cmd.ErrOrStderr()), clients.Profile)
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "Connecting to %s (%s) in %s...\n", inst.Name, inst.ID, clients.Region)
			return authHint(connect.StartSession(ctx, clients.SSMSession, d.runner,
				inst.ID, clients.Region, clients.Profile, endpoint), clients.Profile)
		},
	}
	c.Flags().StringVarP(&profile, "profile", "p", "", "AWS profile (defaults to SDK chain)")
	c.Flags().StringVarP(&region, "region", "r", "", "AWS region (defaults to SDK config)")
	c.Flags().StringVarP(&forwardSpec, "forward", "L", "", "Port-forward spec: comma-separated PORT or LOCAL:REMOTE")
	c.Flags().StringVarP(&host, "host", "H", "", "Remote host reachable from the instance (e.g. an RDS endpoint)")
	c.Flags().StringVarP(&file, "file", "f", "", "Connections file (default ~/.config/aws-tools/connections.config)")
	return c
}

// lookupConnection returns the named connection from path. A missing file
// is not an error (ok=false) — the caller falls back to instance handling.
func lookupConnection(path, name string) (connect.Connection, bool, error) {
	conns, err := connect.LoadConnections(path)
	if err != nil {
		if os.IsNotExist(err) {
			return connect.Connection{}, false, nil
		}
		return connect.Connection{}, false, err
	}
	c, ok := conns[name]
	return c, ok, nil
}

// runForwards starts every port-forward, blocking until all end. Multiple
// forwards run as concurrent plugin processes; they share the terminal
// process group, so one Ctrl+C tears them all down.
// ponytail: shared os.Stdin across the children is benign — port-forward
// sessions don't read interactive stdin the way a shell does.
func runForwards(ctx context.Context, clients *ssmClients, runner connect.PluginRunner, pfs []connect.PortForward, instanceID, endpoint string, logw io.Writer) error {
	if len(pfs) == 1 {
		fmt.Fprintf(logw, "Forwarding %s\n", pfs[0])
		return connect.StartPortForward(ctx, clients.SSMSession, runner, pfs[0], instanceID, clients.Region, clients.Profile, endpoint)
	}
	var wg sync.WaitGroup
	errs := make([]error, len(pfs))
	for i, pf := range pfs {
		fmt.Fprintf(logw, "Forwarding %s\n", pf)
		wg.Add(1)
		go func(i int, pf connect.PortForward) {
			defer wg.Done()
			errs[i] = connect.StartPortForward(ctx, clients.SSMSession, runner, pf, instanceID, clients.Region, clients.Profile, endpoint)
		}(i, pf)
	}
	wg.Wait()
	return errors.Join(errs...)
}

func toInstanceItems(list []connect.Instance) []tui.InstanceItem {
	items := make([]tui.InstanceItem, len(list))
	for i, in := range list {
		items[i] = tui.InstanceItem{ID: in.ID, Name: in.Name, State: in.State, Ping: in.Ping}
	}
	return items
}

// instByID returns the instance with the given ID, or a zero Instance if the
// id isn't in the list (it always is — it came from the same slice).
func instByID(list []connect.Instance, id string) connect.Instance {
	for _, in := range list {
		if in.ID == id {
			return in
		}
	}
	return connect.Instance{}
}

func printInstances(w io.Writer, list []connect.Instance) {
	if len(list) == 0 {
		fmt.Fprintln(w, "(no instances)")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tID\tSTATE\tPING")
	for _, i := range list {
		name := i.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, i.ID, i.State, i.Ping)
	}
	tw.Flush()
}
