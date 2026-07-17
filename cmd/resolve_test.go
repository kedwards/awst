package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/kedwards/awst/v3/internal/tui"
)

func stubList(names ...string) func() ([]string, error) {
	return func() ([]string, error) { return names, nil }
}

func TestEnsureProfile(t *testing.T) {
	pickerCalled := false
	pick := func([]tui.ProfileItem) (string, error) { pickerCalled = true; return "picked", nil }

	t.Run("given value wins", func(t *testing.T) {
		pickerCalled = false
		got, err := ensureProfile("flagval", func() bool { return true }, stubList("a"), pick)
		if err != nil || got != "flagval" {
			t.Fatalf("got %q, err %v", got, err)
		}
		if pickerCalled {
			t.Fatal("picker should not fire when value given")
		}
	})

	t.Run("AWS_PROFILE env", func(t *testing.T) {
		t.Setenv("AWS_PROFILE", "envprof")
		got, _ := ensureProfile("", func() bool { return true }, stubList("a"), pick)
		if got != "envprof" {
			t.Fatalf("got %q, want envprof", got)
		}
	})

	t.Run("non-terminal does not prompt", func(t *testing.T) {
		t.Setenv("AWS_PROFILE", "")
		pickerCalled = false
		got, _ := ensureProfile("", func() bool { return false }, stubList("a"), pick)
		if got != "" || pickerCalled {
			t.Fatalf("non-terminal should return empty without prompting (got %q, called %v)", got, pickerCalled)
		}
	})

	t.Run("terminal prompts", func(t *testing.T) {
		t.Setenv("AWS_PROFILE", "")
		got, _ := ensureProfile("", func() bool { return true }, stubList("a", "b"), pick)
		if got != "picked" {
			t.Fatalf("expected picker result, got %q", got)
		}
	})

	t.Run("aborted propagates", func(t *testing.T) {
		t.Setenv("AWS_PROFILE", "")
		_, err := ensureProfile("", func() bool { return true }, stubList("a"),
			func([]tui.ProfileItem) (string, error) { return "", tui.ErrAborted })
		if !errors.Is(err, tui.ErrAborted) {
			t.Fatalf("expected ErrAborted, got %v", err)
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
