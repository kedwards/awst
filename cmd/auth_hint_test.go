package cmd

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthHint_Nil(t *testing.T) {
	require.NoError(t, authHint(nil, "dev"))
}

func TestAuthHint_NonAuthError_Untouched(t *testing.T) {
	in := errors.New("instance i-0123 not found")
	got := authHint(in, "dev")
	require.Equal(t, in, got, "non-auth errors should pass through unchanged")
}

func TestAuthHint_SSOTokenMissing(t *testing.T) {
	in := errors.New("operation error SSO: GetRoleCredentials, no valid SSO token found in cache")
	got := authHint(in, "dev")
	require.Contains(t, got.Error(), "hint")
	require.Contains(t, got.Error(), "awst login dev")
	require.ErrorIs(t, got, in)
}

func TestAuthHint_ExpiredToken(t *testing.T) {
	in := errors.New("ExpiredTokenException: The security token included in the request is expired")
	got := authHint(in, "prod")
	require.Contains(t, got.Error(), "awst login prod")
}

func TestAuthHint_FailedToRefresh(t *testing.T) {
	in := errors.New("failed to refresh cached credentials, no valid SSO token found")
	got := authHint(in, "dev")
	require.Contains(t, got.Error(), "hint")
}

func TestAuthHint_FailedToRetrieve(t *testing.T) {
	in := errors.New("failed to retrieve credentials: chain exhausted")
	got := authHint(in, "")
	require.Contains(t, got.Error(), "hint")
	require.Contains(t, got.Error(), "awst login <profile>")
}

func TestAuthHint_PlainAccessDenied_Untouched(t *testing.T) {
	// IAM "you can't do this" is a different beast — re-logging in won't fix
	// it, so don't suggest it.
	in := errors.New("AccessDenied: User: arn:aws:iam::1234:user/x is not authorized to perform ec2:DescribeInstances")
	got := authHint(in, "dev")
	require.Equal(t, in, got)
}
