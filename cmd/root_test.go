package cmd

import "testing"

func TestProfileArg(t *testing.T) {
	cases := []struct {
		name    string
		flag    string
		args    []string
		want    string
		wantErr bool
	}{
		{"neither", "", nil, "", false},
		{"positional", "", []string{"dev"}, "dev", false},
		{"flag", "dev", nil, "dev", false},
		{"both conflict", "dev", []string{"dev"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := profileArg(tc.flag, tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
