package console

import (
	"os"
	"path/filepath"
	"testing"
)

func writeExt(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "extensions.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtensionInFile(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "awst containers present and active (matched by id)",
			body: `{"addons":[{"active":true,"id":"awst-containers@kedwards.github.io","defaultLocale":{"name":"awst Containers"}}]}`,
			want: true,
		},
		{
			name: "awst containers inactive is ignored",
			body: `{"addons":[{"active":false,"id":"awst-containers@kedwards.github.io","defaultLocale":{"name":"awst Containers"}}]}`,
			want: false,
		},
		{
			name: "granted containers present and active (transitional fallback)",
			body: `{"addons":[{"active":true,"defaultLocale":{"name":"Granted Containers"}}]}`,
			want: true,
		},
		{
			name: "case-insensitive match",
			body: `{"addons":[{"active":true,"defaultLocale":{"name":"granted containers"}}]}`,
			want: true,
		},
		{
			name: "inactive is ignored",
			body: `{"addons":[{"active":false,"defaultLocale":{"name":"Granted Containers"}}]}`,
			want: false,
		},
		{
			name: "unrelated extension",
			body: `{"addons":[{"active":true,"defaultLocale":{"name":"AWS SSO Containers"}}]}`,
			want: false,
		},
		{
			name: "no addons",
			body: `{"addons":[]}`,
			want: false,
		},
		{
			name: "malformed json",
			body: `{not json`,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extensionInFile(writeExt(t, tc.body)); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtensionInFile_MissingFile(t *testing.T) {
	if extensionInFile(filepath.Join(t.TempDir(), "nope.json")) {
		t.Fatal("missing file should report not-installed")
	}
}
