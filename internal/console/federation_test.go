package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginURL_ConsoleHome(t *testing.T) {
	u := LoginURL("tok123", "us-east-1", "")
	parsed, err := url.Parse(u)
	require.NoError(t, err)
	q := parsed.Query()
	require.Equal(t, "login", q.Get("Action"))
	require.Equal(t, "awst", q.Get("Issuer"))
	require.Equal(t, "tok123", q.Get("SigninToken"))
	require.Equal(t, "https://console.aws.amazon.com/console/home?region=us-east-1", q.Get("Destination"))
}

func TestLoginURL_ServiceHome(t *testing.T) {
	u := LoginURL("tok123", "eu-west-2", "ec2")
	q, err := url.Parse(u)
	require.NoError(t, err)
	require.Equal(t, "https://eu-west-2.console.aws.amazon.com/ec2/home?region=eu-west-2",
		q.Query().Get("Destination"))
}

func TestSigninToken_RoundTrips(t *testing.T) {
	var gotSession string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "getSigninToken", r.URL.Query().Get("Action"))
		gotSession = r.URL.Query().Get("Session")
		_, _ = w.Write([]byte(`{"SigninToken":"signin-abc"}`))
	}))
	defer srv.Close()

	orig := federationEndpoint
	federationEndpoint = srv.URL
	defer func() { federationEndpoint = orig }()

	tok, err := SigninToken(context.Background(), srv.Client(), Credentials{
		AccessKeyID: "AKIA", SecretAccessKey: "secret", SessionToken: "token",
	})
	require.NoError(t, err)
	require.Equal(t, "signin-abc", tok)
	// The session JSON carries the three credential fields under the AWS keys.
	require.Contains(t, gotSession, `"sessionId":"AKIA"`)
	require.Contains(t, gotSession, `"sessionKey":"secret"`)
	require.Contains(t, gotSession, `"sessionToken":"token"`)
}

func TestSigninToken_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad creds", http.StatusForbidden)
	}))
	defer srv.Close()
	orig := federationEndpoint
	federationEndpoint = srv.URL
	defer func() { federationEndpoint = orig }()

	_, err := SigninToken(context.Background(), srv.Client(), Credentials{SessionToken: "t"})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "403"), "error should carry the status: %v", err)
}
