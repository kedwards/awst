package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/charmbracelet/x/term"

	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/regions"
	"github.com/kedwards/awst/v3/internal/runner"
	"github.com/kedwards/awst/v3/internal/sso"
	"github.com/kedwards/awst/v3/internal/tui"
)

// isStdinTerminal reports whether stdin is an interactive terminal — the gate
// for whether an unresolved profile/region should launch a picker or be left
// to the SDK default chain (so pipes/CI keep working unchanged).
func isStdinTerminal() bool { return term.IsTerminal(os.Stdin.Fd()) }

// regionsEffective returns the user's configured regions, or the built-in
// defaults when none are configured.
func regionsEffective() ([]string, error) { return regions.Effective(paths.RegionsFile()) }

// lookupProfileRegion reads the region pinned by a profile's `region=` in
// ~/.aws/config (no network, no client build). A var so tests can stub it.
var lookupProfileRegion = func(ctx context.Context, profile string) string {
	sc, err := config.LoadSharedConfigProfile(ctx, profile)
	if err != nil {
		return ""
	}
	return sc.Region
}

// ensureProfile resolves the AWS profile, prompting with a picker only when it
// can't be resolved and stdin is interactive. Resolution order:
// given value → AWS_PROFILE → picker → "" (let the SDK default chain decide).
func ensureProfile(in string, isTerminal func() bool,
	list func() ([]string, error), pick func([]tui.ProfileItem) (string, error)) (string, error) {
	if in != "" {
		return in, nil
	}
	if env := os.Getenv("AWS_PROFILE"); env != "" {
		return env, nil
	}
	if isTerminal == nil || !isTerminal() {
		return "", nil
	}
	names, err := list()
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", nil
	}
	items := make([]tui.ProfileItem, len(names))
	for i, n := range names {
		items[i] = tui.ProfileItem{Profile: n}
	}
	return pick(items)
}

// ensureRegion resolves the AWS region, prompting with a picker only when it
// can't be resolved and stdin is interactive. Resolution order:
// regionFlag → AWS_REGION/AWS_DEFAULT_REGION → profile's region= → picker →
// "" (keep each command's existing fallback).
func ensureRegion(ctx context.Context, profile, regionFlag string, isTerminal func() bool,
	regionList func() ([]string, error), pick func([]string) (string, error)) (string, error) {
	if regionFlag != "" {
		return regionFlag, nil
	}
	if env := envOr("AWS_REGION", os.Getenv("AWS_DEFAULT_REGION")); env != "" {
		return env, nil
	}
	if profile != "" {
		if r := lookupProfileRegion(ctx, profile); r != "" {
			return r, nil
		}
	}
	if isTerminal == nil || !isTerminal() {
		return "", nil
	}
	list, err := regionList()
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", nil
	}
	return pick(list)
}

// resolveProfileRegion resolves the profile first, then the region (skipping the
// region picker when it's already known). Used by the commands that need both.
func resolveProfileRegion(ctx context.Context, profile, region string, isTerminal func() bool) (string, string, error) {
	p, err := ensureProfile(profile, isTerminal, defaultListProfiles, tui.SelectProfile)
	if err != nil {
		return "", "", err
	}
	r, err := ensureRegion(ctx, p, region, isTerminal, regionsEffective, tui.SelectRegion)
	if err != nil {
		return "", "", err
	}
	return p, r, nil
}

// ssoLogin holds the SSO device-flow collaborators shared by the commands that
// auto-login (console, connect, run). Its ensure method is the single copy of
// the "log in if the cached token is missing or expired" logic.
type ssoLogin struct {
	cache         *sso.Cache
	sessionLoader func(ctx context.Context, profile, configFile string) (sso.SSOSession, error)
	oidcFactory   func(ctx context.Context, region string) (sso.OIDCClient, error)
	openBrowser   func(string) error
	sleep         func(time.Duration)
	now           func() time.Time
}

// ensure guarantees a valid SSO token for an SSO profile, running the device
// flow when needed. A profile without an sso_session (static/env creds) is a
// no-op — credential resolution handles it. Prompts go to errOut; the browser
// is opened unless noBrowser is set.
func (s ssoLogin) ensure(ctx context.Context, errOut io.Writer, profile string, noBrowser bool) error {
	if s.cache == nil || s.sessionLoader == nil {
		return nil
	}
	sess, err := s.sessionLoader(ctx, profile, "")
	if err != nil {
		return nil // not an SSO profile; let credential resolution proceed
	}
	prompt := func(uri, code string) {
		fmt.Fprintf(errOut,
			"Open this URL in your browser to authorize awst:\n  %s\nUser code: %s\n", uri, code)
		if !noBrowser && s.openBrowser != nil {
			_ = s.openBrowser(uri)
		}
	}
	_, _, err = sso.EnsureToken(ctx, s.cache, sess,
		func() (sso.OIDCClient, error) { return s.oidcFactory(ctx, sess.Region) },
		prompt, s.sleep, s.now)
	return err
}

// resolveTargetsInteractive builds the run targets when no filter was given:
// multi-select the profiles, then pick a region per selected profile. It errors
// (rather than falling back to "all profiles") when stdin isn't a terminal, so
// pipes/CI must pass an explicit filter. Pickers are injected for testing.
func resolveTargetsInteractive(ctx context.Context,
	isTerminal func() bool,
	listProfiles func() ([]string, error),
	pickProfiles func([]tui.ProfileItem) ([]string, error),
	regionList func() ([]string, error),
	pickRegion func(profile string, regions []string) (string, error),
) ([]runner.Target, error) {
	if isTerminal == nil || !isTerminal() {
		return nil, errors.New(`no profile:region filter given and stdin is not a terminal; pass targets like "dev:us-east-1"`)
	}
	names, err := listProfiles()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New("no profiles found in ~/.aws/config")
	}
	items := make([]tui.ProfileItem, len(names))
	for i, n := range names {
		items[i] = tui.ProfileItem{Profile: n}
	}
	chosen, err := pickProfiles(items)
	if err != nil {
		return nil, err // may be tui.ErrAborted
	}
	regionsList, err := regionList()
	if err != nil {
		return nil, err
	}
	out := make([]runner.Target, 0, len(chosen))
	for _, p := range chosen {
		r, err := pickRegion(p, regionsList)
		if err != nil {
			return nil, err // may be tui.ErrAborted
		}
		out = append(out, runner.Target{Profile: p, Region: r})
	}
	return out, nil
}
