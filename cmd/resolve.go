package cmd

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/charmbracelet/x/term"

	"github.com/kedwards/awst/v3/internal/paths"
	"github.com/kedwards/awst/v3/internal/regions"
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
