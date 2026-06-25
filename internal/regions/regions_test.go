package regions

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoad_MissingIsNil(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "nope.config"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("missing file should load nil, got %v", got)
	}
}

func TestLoad_IgnoresCommentsAndBlanks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "regions.config")
	os.WriteFile(p, []byte("# header\n\nus-west-2\n; semi\n  eu-west-1  \n"), 0o644)
	got, err := Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"us-west-2", "eu-west-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEffective_FallsBackToDefault(t *testing.T) {
	got, err := Effective(filepath.Join(t.TempDir(), "missing.config"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, Default) {
		t.Fatalf("empty user list should yield Default")
	}
}

func TestAddRemove_RoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "regions.config")

	if added, _ := Add(p, "us-west-2"); !added {
		t.Fatal("first add should report added")
	}
	if added, _ := Add(p, "us-west-2"); added {
		t.Fatal("duplicate add should report not-added")
	}
	Add(p, "ca-central-1")

	got, _ := Load(p)
	want := []string{"ca-central-1", "us-west-2"} // sorted
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	if removed, _ := Remove(p, "us-west-2"); !removed {
		t.Fatal("remove of present region should report removed")
	}
	if removed, _ := Remove(p, "us-west-2"); removed {
		t.Fatal("remove of absent region should report not-removed")
	}
	got, _ = Load(p)
	if !reflect.DeepEqual(got, []string{"ca-central-1"}) {
		t.Fatalf("after remove got %v", got)
	}
}
