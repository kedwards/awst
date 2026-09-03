package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/kedwards/awst/v3/internal/tui"
)

func stubList(names ...string) func() ([]string, error) {
	return func() ([]string, error) { return names, nil }
}

// setTestProfiles stubs defaultListProfiles for the duration of t, restoring
// the original when the test completes.
func setTestProfiles(t *testing.T, names ...string) {
	t.Helper()
	orig := defaultListProfiles
	defaultListProfiles = func() ([]string, error) { return names, nil }
	t.Cleanup(func() { defaultListProfiles = orig })
}

func TestEnsureProfile(t *testing.T) {
	pickerCalled := false
	pick := func([]tui.ProfileItem) (string, error) { pickerCalled = true; return "picked", nil }

	t.Run("exact match wins", func(t *testing.T) {
		pickerCalled = false
		got, err := ensureProfile(io.Discard, "rch-platform-dev-ninja", func() bool { return true },
			stubList("rch-platform-dev-ninja", "rch-platform-prod-ninja"), pick)
		if err != nil || got != "rch-platform-dev-ninja" {
			t.Fatalf("got %q, err %v", got, err)
		}
		if pickerCalled {
			t.Fatal("picker should not fire on exact match")
		}
	})

	t.Run("substring match auto-selects one", func(t *testing.T) {
		pickerCalled = false
		got, err := ensureProfile(io.Discard, "ninja", func() bool { return true },
			stubList("rch-platform-dev-ninja", "rch-platform-prod-ninja"), pick)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		// Multiple matches → picker is called with the subset.
		if !pickerCalled {
			t.Fatal("picker should fire for multiple substring matches")
		}
		if got != "picked" {
			t.Fatalf("expected picker result, got %q", got)
		}
	})

	t.Run("single substring match auto-selects", func(t *testing.T) {
		pickerCalled = false
		var out bytes.Buffer
		got, err := ensureProfile(&out, "ninja", func() bool { return true },
			stubList("rch-platform-dev-ninja"), pick)
		if err != nil || got != "rch-platform-dev-ninja" {
			t.Fatalf("got %q, err %v", got, err)
		}
		if pickerCalled {
			t.Fatal("picker should not fire for single substring match")
		}
		if !strings.Contains(out.String(), `"ninja" matched "rch-platform-dev-ninja"`) {
			t.Fatalf("expected a match note on w, got: %q", out.String())
		}
	})

	t.Run("no match returns original", func(t *testing.T) {
		pickerCalled = false
		got, err := ensureProfile(io.Discard, "ghost", func() bool { return true },
			stubList("rch-platform-dev-ninja"), pick)
		if err != nil || got != "ghost" {
			t.Fatalf("got %q, err %v", got, err)
		}
		if pickerCalled {
			t.Fatal("picker should not fire when no matches")
		}
	})

	t.Run("AWS_PROFILE env", func(t *testing.T) {
		t.Setenv("AWS_PROFILE", "envprof")
		got, _ := ensureProfile(io.Discard, "", func() bool { return true }, stubList("a"), pick)
		if got != "envprof" {
			t.Fatalf("got %q, want envprof", got)
		}
	})

	t.Run("non-terminal does not prompt", func(t *testing.T) {
		t.Setenv("AWS_PROFILE", "")
		pickerCalled = false
		got, _ := ensureProfile(io.Discard, "", func() bool { return false }, stubList("a"), pick)
		if got != "" || pickerCalled {
			t.Fatalf("non-terminal should return empty without prompting (got %q, called %v)", got, pickerCalled)
		}
	})

	t.Run("terminal prompts", func(t *testing.T) {
		t.Setenv("AWS_PROFILE", "")
		got, _ := ensureProfile(io.Discard, "", func() bool { return true }, stubList("a", "b"), pick)
		if got != "picked" {
			t.Fatalf("expected picker result, got %q", got)
		}
	})

	t.Run("aborted propagates", func(t *testing.T) {
		t.Setenv("AWS_PROFILE", "")
		_, err := ensureProfile(io.Discard, "", func() bool { return true }, stubList("a"),
			func([]tui.ProfileItem) (string, error) { return "", tui.ErrAborted })
		if !errors.Is(err, tui.ErrAborted) {
			t.Fatalf("expected ErrAborted, got %v", err)
		}
	})
}

func TestMatchProfile(t *testing.T) {
	pick := func([]tui.ProfileItem) (string, error) { return "picked", nil }
	profiles := stubList("rch-platform-dev-ninja", "rch-platform-prod-ninja", "rch-platform-dev-coffee")

	t.Run("exact match", func(t *testing.T) {
		got, err := matchProfile(io.Discard, "rch-platform-dev-ninja", func() bool { return true }, profiles, pick)
		if err != nil || got != "rch-platform-dev-ninja" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("exact match takes precedence over an ambiguous substring", func(t *testing.T) {
		// "rch-platform-dev-ninja" is an exact hit, but as a bare substring it
		// would also match "rch-platform-prod-ninja" — the exact check must
		// short-circuit before the substring scan ever runs, so no picker.
		got, err := matchProfile(io.Discard, "rch-platform-dev-ninja", func() bool { return true },
			profiles, func([]tui.ProfileItem) (string, error) {
				t.Fatal("picker should not fire when the input is an exact match")
				return "", nil
			})
		if err != nil || got != "rch-platform-dev-ninja" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("single substring match auto-selects and notes it on w", func(t *testing.T) {
		var out bytes.Buffer
		got, err := matchProfile(&out, "coffee", func() bool { return true }, profiles, pick)
		if err != nil || got != "rch-platform-dev-coffee" {
			t.Fatalf("got %q, err %v", got, err)
		}
		if !strings.Contains(out.String(), `"coffee" matched "rch-platform-dev-coffee"`) {
			t.Fatalf("expected a match note on w, got: %q", out.String())
		}
	})

	t.Run("nil writer does not panic", func(t *testing.T) {
		got, err := matchProfile(nil, "coffee", func() bool { return true }, profiles, pick)
		if err != nil || got != "rch-platform-dev-coffee" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("case-insensitive single match auto-selects", func(t *testing.T) {
		got, err := matchProfile(io.Discard, "COFFEE", func() bool { return true }, profiles, pick)
		if err != nil || got != "rch-platform-dev-coffee" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("case-insensitive ambiguous match", func(t *testing.T) {
		got, err := matchProfile(io.Discard, "NINJA", func() bool { return true }, profiles, pick)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		// Multiple "ninja" matches → picker called.
		if got != "picked" {
			t.Fatalf("expected picker result, got %q", got)
		}
	})

	t.Run("multiple matches terminal picks", func(t *testing.T) {
		got, err := matchProfile(io.Discard, "ninja", func() bool { return true }, profiles, pick)
		if err != nil || got != "picked" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("multiple matches non-terminal errors", func(t *testing.T) {
		_, err := matchProfile(io.Discard, "ninja", func() bool { return false }, profiles, pick)
		if err == nil {
			t.Fatal("expected error for multiple matches in non-terminal")
		}
		if !strings.Contains(err.Error(), "multiple profiles") {
			t.Fatalf("expected 'multiple profiles' in error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "rch-platform-dev-ninja") || !strings.Contains(err.Error(), "rch-platform-prod-ninja") {
			t.Fatalf("expected both candidate names listed in the error, got: %v", err)
		}
	})

	t.Run("no match returns original", func(t *testing.T) {
		got, err := matchProfile(io.Discard, "ghost", func() bool { return true }, profiles, pick)
		if err != nil || got != "ghost" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("empty list returns original", func(t *testing.T) {
		got, err := matchProfile(io.Discard, "ninja", func() bool { return true }, stubList(), pick)
		if err != nil || got != "ninja" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("list error returns original", func(t *testing.T) {
		badList := func() ([]string, error) { return nil, errors.New("disk error") }
		got, err := matchProfile(io.Discard, "ninja", func() bool { return true }, badList, pick)
		if err != nil || got != "ninja" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})
}

func TestEnsureRegion(t *testing.T) {
	ctx := context.Background()
	pick := func([]string) (string, error) { return "picked-region", nil }
	regions := stubList("us-west-2", "eu-west-1")

	// Default: no profile region resolvable.
	orig := lookupProfileRegion
	t.Cleanup(func() { lookupProfileRegion = orig })
	lookupProfileRegion = func(context.Context, string) string { return "" }

	t.Run("flag wins", func(t *testing.T) {
		got, _ := ensureRegion(ctx, "p", "us-east-2", func() bool { return true }, regions, pick)
		if got != "us-east-2" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("env wins over profile/picker", func(t *testing.T) {
		t.Setenv("AWS_REGION", "ap-south-1")
		got, _ := ensureRegion(ctx, "p", "", func() bool { return true }, regions, pick)
		if got != "ap-south-1" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("profile region skips picker", func(t *testing.T) {
		t.Setenv("AWS_REGION", "")
		t.Setenv("AWS_DEFAULT_REGION", "")
		lookupProfileRegion = func(context.Context, string) string { return "ca-central-1" }
		defer func() { lookupProfileRegion = func(context.Context, string) string { return "" } }()
		got, _ := ensureRegion(ctx, "p", "", func() bool { return true }, regions, pick)
		if got != "ca-central-1" {
			t.Fatalf("profile region should be used, got %q", got)
		}
	})

	t.Run("non-terminal does not prompt", func(t *testing.T) {
		t.Setenv("AWS_REGION", "")
		t.Setenv("AWS_DEFAULT_REGION", "")
		got, _ := ensureRegion(ctx, "p", "", func() bool { return false }, regions, pick)
		if got != "" {
			t.Fatalf("non-terminal should return empty, got %q", got)
		}
	})

	t.Run("terminal prompts when unresolved", func(t *testing.T) {
		t.Setenv("AWS_REGION", "")
		t.Setenv("AWS_DEFAULT_REGION", "")
		got, _ := ensureRegion(ctx, "p", "", func() bool { return true }, regions, pick)
		if got != "picked-region" {
			t.Fatalf("expected picker result, got %q", got)
		}
	})
}

func TestResolveTargetsInteractive(t *testing.T) {
	ctx := context.Background()
	profiles := stubList("coffee", "wtf")
	regionList := func() ([]string, error) { return []string{"us-east-1", "us-west-2"}, nil }

	t.Run("region per profile", func(t *testing.T) {
		pickProfiles := func([]tui.ProfileItem) ([]string, error) { return []string{"coffee", "wtf"}, nil }
		pickRegion := func(profile string, _ []string) (string, error) {
			if profile == "coffee" {
				return "us-east-1", nil
			}
			return "us-west-2", nil
		}
		got, err := resolveTargetsInteractive(ctx, func() bool { return true }, profiles, pickProfiles, regionList, pickRegion)
		if err != nil {
			t.Fatalf("err %v", err)
		}
		if len(got) != 2 || got[0].Profile != "coffee" || got[0].Region != "us-east-1" ||
			got[1].Profile != "wtf" || got[1].Region != "us-west-2" {
			t.Fatalf("unexpected targets: %+v", got)
		}
	})

	t.Run("not a terminal errors", func(t *testing.T) {
		pickProfiles := func([]tui.ProfileItem) ([]string, error) { t.Fatal("picker should not fire"); return nil, nil }
		pickRegion := func(string, []string) (string, error) { return "", nil }
		_, err := resolveTargetsInteractive(ctx, func() bool { return false }, profiles, pickProfiles, regionList, pickRegion)
		if err == nil {
			t.Fatal("expected an error when stdin is not a terminal")
		}
	})

	t.Run("abort propagates", func(t *testing.T) {
		pickProfiles := func([]tui.ProfileItem) ([]string, error) { return nil, tui.ErrAborted }
		pickRegion := func(string, []string) (string, error) { return "", nil }
		_, err := resolveTargetsInteractive(ctx, func() bool { return true }, profiles, pickProfiles, regionList, pickRegion)
		if !errors.Is(err, tui.ErrAborted) {
			t.Fatalf("expected ErrAborted, got %v", err)
		}
	})
}
