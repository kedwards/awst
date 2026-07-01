package sso

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
)

const (
	clientName       = "awst"
	clientType       = "public"
	deviceGrantType  = "urn:ietf:params:oauth:grant-type:device_code"
	slowDownIncrease = 5 * time.Second
)

// OIDCClient is the subset of *ssooidc.Client used by Login. Kept local so
// tests can stub it.
type OIDCClient interface {
	RegisterClient(ctx context.Context, in *ssooidc.RegisterClientInput, optFns ...func(*ssooidc.Options)) (*ssooidc.RegisterClientOutput, error)
	StartDeviceAuthorization(ctx context.Context, in *ssooidc.StartDeviceAuthorizationInput, optFns ...func(*ssooidc.Options)) (*ssooidc.StartDeviceAuthorizationOutput, error)
	CreateToken(ctx context.Context, in *ssooidc.CreateTokenInput, optFns ...func(*ssooidc.Options)) (*ssooidc.CreateTokenOutput, error)
}

// Prompter is called once with the verification URL (the "complete" form
// with the user code baked in) and the user code, so the command layer can
// print the prompt and optionally open a browser.
type Prompter func(verificationURIComplete, userCode string)

// EnsureToken returns a valid SSO token for sess: it reuses the cached token
// when one exists and has not expired, otherwise it builds an OIDC client (via
// newOIDC, called only when a login is actually needed), runs the device flow,
// caches the result, and returns it. The bool reports whether the returned
// token came from cache.
func EnsureToken(
	ctx context.Context,
	cache *Cache,
	sess SSOSession,
	newOIDC func() (OIDCClient, error),
	prompt Prompter,
	sleep func(time.Duration),
	now func() time.Time,
) (Token, bool, error) {
	if now == nil {
		now = time.Now
	}
	if tok, err := cache.Load(sess.Name); err == nil && tok.AccessToken != "" && tok.ExpiresAt.After(now().Add(5*time.Minute)) {
		return tok, true, nil
	}

	oidc, err := newOIDC()
	if err != nil {
		return Token{}, false, err
	}
	tok, err := Login(ctx, oidc, sess, prompt, sleep, now)
	if err != nil {
		return Token{}, false, err
	}
	if err := cache.Save(sess.Name, tok); err != nil {
		return Token{}, false, err
	}
	return tok, false, nil
}

// Login runs the SSO OIDC device-authorization flow against oidc. sleep and
// now are injected so polling can be tested without real waits.
func Login(
	ctx context.Context,
	oidc OIDCClient,
	sess SSOSession,
	prompt Prompter,
	sleep func(time.Duration),
	now func() time.Time,
) (Token, error) {
	if sleep == nil {
		sleep = time.Sleep
	}
	if now == nil {
		now = time.Now
	}

	reg, err := oidc.RegisterClient(ctx, &ssooidc.RegisterClientInput{
		ClientName: aws.String(clientName),
		ClientType: aws.String(clientType),
		Scopes:     []string{"sso:account:access"},
	})
	if err != nil {
		return Token{}, fmt.Errorf("register client: %w", err)
	}

	dev, err := oidc.StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
		ClientId:     reg.ClientId,
		ClientSecret: reg.ClientSecret,
		StartUrl:     aws.String(sess.StartURL),
	})
	if err != nil {
		return Token{}, fmt.Errorf("start device authorization: %w", err)
	}

	if prompt != nil {
		prompt(aws.ToString(dev.VerificationUriComplete), aws.ToString(dev.UserCode))
	}

	interval := time.Duration(dev.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := now().Add(time.Duration(dev.ExpiresIn) * time.Second)

	for {
		if !now().Before(deadline) {
			return Token{}, errors.New("device authorization expired before user completed login")
		}
		sleep(interval)

		out, err := oidc.CreateToken(ctx, &ssooidc.CreateTokenInput{
			ClientId:     reg.ClientId,
			ClientSecret: reg.ClientSecret,
			GrantType:    aws.String(deviceGrantType),
			DeviceCode:   dev.DeviceCode,
		})
		if err == nil {
			return Token{
				AccessToken:  aws.ToString(out.AccessToken),
				ExpiresAt:    now().Add(time.Duration(out.ExpiresIn) * time.Second),
				RefreshToken: aws.ToString(out.RefreshToken),
				ClientID:     aws.ToString(reg.ClientId),
				ClientSecret: aws.ToString(reg.ClientSecret),
			}, nil
		}

		var pending *types.AuthorizationPendingException
		var slow *types.SlowDownException
		var expired *types.ExpiredTokenException
		var denied *types.AccessDeniedException
		switch {
		case errors.As(err, &pending):
			continue
		case errors.As(err, &slow):
			interval += slowDownIncrease
			continue
		case errors.As(err, &expired):
			return Token{}, fmt.Errorf("device authorization expired: %w", err)
		case errors.As(err, &denied):
			return Token{}, fmt.Errorf("access denied by IdP: %w", err)
		default:
			return Token{}, fmt.Errorf("poll for token: %w", err)
		}
	}
}
