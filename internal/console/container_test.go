package console

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainerURL_FormatAndStableColor(t *testing.T) {
	target := "https://signin.aws.amazon.com/federation?Action=login&SigninToken=abc"
	got := ContainerURL("dev", target)

	require.True(t, strings.HasPrefix(got, "ext+awst-containers:"), "got %q", got)

	// Parse the params after the protocol prefix.
	q, err := url.ParseQuery(strings.TrimPrefix(got, "ext+awst-containers:"))
	require.NoError(t, err)
	require.Equal(t, "dev", q.Get("name"))
	require.Equal(t, target, q.Get("url"), "url param must round-trip the federation URL")
	require.Equal(t, "fingerprint", q.Get("icon"))
	require.True(t, slices.Contains(containerColors, q.Get("color")), "color %q must be in the palette", q.Get("color"))

	// Same name -> same color (stable across runs).
	require.Equal(t, got, ContainerURL("dev", target))
}

func TestContainerURL_DifferentNamesUsePalette(t *testing.T) {
	// Not all names differ in color (only 8 buckets), but each must be valid.
	for _, name := range []string{"rch-platform-dev-coffee", "rch-platform-dev-wtf", "prod"} {
		q, err := url.ParseQuery(strings.TrimPrefix(ContainerURL(name, "https://x"), "ext+awst-containers:"))
		require.NoError(t, err)
		require.True(t, slices.Contains(containerColors, q.Get("color")))
	}
}
