// Package console builds AWS web-console sign-in URLs from temporary
// credentials using the AWS federation endpoint — the same flow `assume -c`
// uses. Only the standard `aws` partition is supported.
//
// ponytail: GovCloud (signin.amazonaws-us-gov.com) and China
// (signin.amazonaws.cn) use different signin hosts — add per-partition hosts
// here if those partitions are ever needed.
package console

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// federationEndpoint is overridable in tests to point at an httptest server.
var federationEndpoint = "https://signin.aws.amazon.com/federation"

// Credentials are the temporary credentials federated into the console. A
// SessionToken is required — the console federation flow only accepts session
// (STS/SSO) credentials, not long-term IAM user keys.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// SigninToken exchanges temporary credentials for a console sign-in token via
// the AWS federation endpoint.
func SigninToken(ctx context.Context, hc *http.Client, c Credentials) (string, error) {
	session, err := json.Marshal(struct {
		SessionID    string `json:"sessionId"`
		SessionKey   string `json:"sessionKey"`
		SessionToken string `json:"sessionToken"`
	}{c.AccessKeyID, c.SecretAccessKey, c.SessionToken})
	if err != nil {
		return "", fmt.Errorf("encode session: %w", err)
	}

	q := url.Values{
		"Action":  {"getSigninToken"},
		"Session": {string(session)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, federationEndpoint+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("federation request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("federation getSigninToken: %s: %s", resp.Status, body)
	}

	var out struct {
		SigninToken string `json:"SigninToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode signin token: %w", err)
	}
	if out.SigninToken == "" {
		return "", fmt.Errorf("federation returned an empty signin token")
	}
	return out.SigninToken, nil
}

// LoginURL builds the federated console URL. With service == "" it targets the
// console home; otherwise the named service's regional console home (e.g.
// "ec2" -> https://<region>.console.aws.amazon.com/ec2/home?region=<region>).
func LoginURL(signinToken, region, service string) string {
	var dest string
	if service == "" {
		dest = fmt.Sprintf("https://console.aws.amazon.com/console/home?region=%s", url.QueryEscape(region))
	} else {
		dest = fmt.Sprintf("https://%s.console.aws.amazon.com/%s/home?region=%s",
			url.PathEscape(region), url.PathEscape(service), url.QueryEscape(region))
	}

	q := url.Values{
		"Action":      {"login"},
		"Issuer":      {"awst"},
		"Destination": {dest},
		"SigninToken": {signinToken},
	}
	return federationEndpoint + "?" + q.Encode()
}
