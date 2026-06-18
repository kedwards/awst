package creds

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	creds aws.Credentials
	err   error
}

func (f fakeProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	return f.creds, f.err
}

func TestResolve_UsesInjectedProvider(t *testing.T) {
	got, err := Resolve(context.Background(), "dev", fakeProvider{
		creds: aws.Credentials{
			AccessKeyID:     "AKIA",
			SecretAccessKey: "secret",
			SessionToken:    "token",
		},
	})

	require.NoError(t, err)
	require.Equal(t, Credentials{
		AccessKeyID:     "AKIA",
		SecretAccessKey: "secret",
		SessionToken:    "token",
	}, got)
}

func TestResolve_PropagatesError(t *testing.T) {
	sentinel := errors.New("no SSO session")

	_, err := Resolve(context.Background(), "dev", fakeProvider{err: sentinel})

	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), `"dev"`, "error should name the profile")
}
