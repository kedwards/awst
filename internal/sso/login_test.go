package sso

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
	"github.com/stretchr/testify/require"
)

type stubOIDC struct {
	regOut  *ssooidc.RegisterClientOutput
	regErr  error
	devOut  *ssooidc.StartDeviceAuthorizationOutput
	devErr  error
	tokOuts []*ssooidc.CreateTokenOutput
	tokErrs []error
	tokN    int

	regIn *ssooidc.RegisterClientInput
	devIn *ssooidc.StartDeviceAuthorizationInput
	tokIn []*ssooidc.CreateTokenInput
}

func (s *stubOIDC) RegisterClient(_ context.Context, in *ssooidc.RegisterClientInput, _ ...func(*ssooidc.Options)) (*ssooidc.RegisterClientOutput, error) {
	s.regIn = in
	return s.regOut, s.regErr
}

func (s *stubOIDC) StartDeviceAuthorization(_ context.Context, in *ssooidc.StartDeviceAuthorizationInput, _ ...func(*ssooidc.Options)) (*ssooidc.StartDeviceAuthorizationOutput, error) {
	s.devIn = in
	return s.devOut, s.devErr
}

func (s *stubOIDC) CreateToken(_ context.Context, in *ssooidc.CreateTokenInput, _ ...func(*ssooidc.Options)) (*ssooidc.CreateTokenOutput, error) {
	s.tokIn = append(s.tokIn, in)
	i := s.tokN
	s.tokN++
	if i < len(s.tokErrs) && s.tokErrs[i] != nil {
		return nil, s.tokErrs[i]
	}
	if i < len(s.tokOuts) {
		return s.tokOuts[i], nil
	}
	return nil, errors.New("stub exhausted")
}

func okRegister() *ssooidc.RegisterClientOutput {
	return &ssooidc.RegisterClientOutput{
		ClientId:     aws.String("cid"),
		ClientSecret: aws.String("csec"),
	}
}

func okDevice() *ssooidc.StartDeviceAuthorizationOutput {
	return &ssooidc.StartDeviceAuthorizationOutput{
		DeviceCode:              aws.String("dev-code"),
		UserCode:                aws.String("ABCD-EFGH"),
		VerificationUriComplete: aws.String("https://example.aws/device?user_code=ABCD-EFGH"),
		Interval:                1,
		ExpiresIn:               600,
	}
}

func okToken() *ssooidc.CreateTokenOutput {
	return &ssooidc.CreateTokenOutput{
		AccessToken:  aws.String("atk"),
		RefreshToken: aws.String("rt"),
		ExpiresIn:    3600,
	}
}

type captureSleep struct{ calls []time.Duration }

func (c *captureSleep) sleep(d time.Duration) { c.calls = append(c.calls, d) }

func TestLogin_HappyPath(t *testing.T) {
	oidc := &stubOIDC{
		regOut:  okRegister(),
		devOut:  okDevice(),
		tokOuts: []*ssooidc.CreateTokenOutput{okToken()},
		tokErrs: []error{nil},
	}
	var promptURL, promptCode string
	prompt := func(uri, code string) {
		promptURL, promptCode = uri, code
	}
	sleep := &captureSleep{}
	now := func() time.Time { return time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC) }

	tok, err := Login(context.Background(),
		oidc,
		SSOSession{Name: "my-sso", Region: "us-east-1", StartURL: "https://example.aws/start"},
		prompt, sleep.sleep, now,
	)

	require.NoError(t, err)
	require.Equal(t, "https://example.aws/device?user_code=ABCD-EFGH", promptURL)
	require.Equal(t, "ABCD-EFGH", promptCode)
	require.Equal(t, Token{
		AccessToken:  "atk",
		ExpiresAt:    now().Add(3600 * time.Second),
		RefreshToken: "rt",
		ClientID:     "cid",
		ClientSecret: "csec",
	}, tok)

	require.Equal(t, "https://example.aws/start", aws.ToString(oidc.devIn.StartUrl))
	require.Equal(t, "cid", aws.ToString(oidc.devIn.ClientId))
	require.Equal(t, "dev-code", aws.ToString(oidc.tokIn[0].DeviceCode))
	require.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", aws.ToString(oidc.tokIn[0].GrantType))

	require.Equal(t, []time.Duration{time.Second}, sleep.calls)
}

func TestLogin_PendingThenSuccess(t *testing.T) {
	oidc := &stubOIDC{
		regOut: okRegister(),
		devOut: okDevice(),
		tokOuts: []*ssooidc.CreateTokenOutput{
			nil, nil, okToken(),
		},
		tokErrs: []error{
			&types.AuthorizationPendingException{},
			&types.AuthorizationPendingException{},
			nil,
		},
	}
	sleep := &captureSleep{}
	tok, err := Login(context.Background(), oidc,
		SSOSession{Name: "s", Region: "r", StartURL: "u"},
		func(string, string) {}, sleep.sleep, time.Now,
	)

	require.NoError(t, err)
	require.Equal(t, "atk", tok.AccessToken)
	require.Equal(t, 3, len(sleep.calls))
	for _, d := range sleep.calls {
		require.Equal(t, time.Second, d)
	}
}

func TestLogin_SlowDownBumpsInterval(t *testing.T) {
	oidc := &stubOIDC{
		regOut: okRegister(),
		devOut: okDevice(),
		tokOuts: []*ssooidc.CreateTokenOutput{
			nil, okToken(),
		},
		tokErrs: []error{
			&types.SlowDownException{},
			nil,
		},
	}
	sleep := &captureSleep{}
	_, err := Login(context.Background(), oidc,
		SSOSession{Name: "s", Region: "r", StartURL: "u"},
		func(string, string) {}, sleep.sleep, time.Now,
	)

	require.NoError(t, err)
	require.Equal(t, []time.Duration{time.Second, 6 * time.Second}, sleep.calls)
}

func TestLogin_ExpiredTokenFails(t *testing.T) {
	oidc := &stubOIDC{
		regOut:  okRegister(),
		devOut:  okDevice(),
		tokOuts: []*ssooidc.CreateTokenOutput{nil},
		tokErrs: []error{&types.ExpiredTokenException{}},
	}
	_, err := Login(context.Background(), oidc,
		SSOSession{Name: "s", Region: "r", StartURL: "u"},
		func(string, string) {}, func(time.Duration) {}, time.Now,
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "expired")
}

func TestLogin_AccessDeniedFails(t *testing.T) {
	oidc := &stubOIDC{
		regOut:  okRegister(),
		devOut:  okDevice(),
		tokOuts: []*ssooidc.CreateTokenOutput{nil},
		tokErrs: []error{&types.AccessDeniedException{}},
	}
	_, err := Login(context.Background(), oidc,
		SSOSession{Name: "s", Region: "r", StartURL: "u"},
		func(string, string) {}, func(time.Duration) {}, time.Now,
	)

	require.Error(t, err)
}

func TestLogin_RegisterErrorPropagates(t *testing.T) {
	sentinel := errors.New("network down")
	oidc := &stubOIDC{regErr: sentinel}

	_, err := Login(context.Background(), oidc,
		SSOSession{Name: "s", Region: "r", StartURL: "u"},
		func(string, string) {}, func(time.Duration) {}, time.Now,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
}
