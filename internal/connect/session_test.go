package connect

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/stretchr/testify/require"
)

type stubSession struct {
	out      *ssm.StartSessionOutput
	err      error
	gotInput *ssm.StartSessionInput
}

func (s *stubSession) StartSession(_ context.Context, in *ssm.StartSessionInput, _ ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	s.gotInput = in
	return s.out, s.err
}

type captureRunner struct {
	gotArgs []string
	err     error
}

func (c *captureRunner) Run(args []string) error {
	c.gotArgs = args
	return c.err
}

func (c *captureRunner) Start(args []string, _ string) (int, error) {
	c.gotArgs = args
	return 4242, c.err
}

func TestStartSession_PassesPluginArgs(t *testing.T) {
	s := &stubSession{out: &ssm.StartSessionOutput{
		SessionId:  aws.String("sess-123"),
		StreamUrl:  aws.String("wss://example/stream"),
		TokenValue: aws.String("tk"),
	}}
	r := &captureRunner{}

	err := StartSession(context.Background(), s, r, "i-abc", "us-east-1", "dev", "https://ssm.us-east-1.amazonaws.com")
	require.NoError(t, err)

	require.Equal(t, "i-abc", aws.ToString(s.gotInput.Target))

	require.Len(t, r.gotArgs, 6)

	var resp map[string]string
	require.NoError(t, json.Unmarshal([]byte(r.gotArgs[0]), &resp))
	require.Equal(t, "sess-123", resp["SessionId"])
	require.Equal(t, "wss://example/stream", resp["StreamUrl"])
	require.Equal(t, "tk", resp["TokenValue"])

	require.Equal(t, "us-east-1", r.gotArgs[1])
	require.Equal(t, "StartSession", r.gotArgs[2])
	require.Equal(t, "dev", r.gotArgs[3])

	var params map[string]string
	require.NoError(t, json.Unmarshal([]byte(r.gotArgs[4]), &params))
	require.Equal(t, "i-abc", params["Target"])

	require.Equal(t, "https://ssm.us-east-1.amazonaws.com", r.gotArgs[5])
}

func TestStartSession_StartSessionError(t *testing.T) {
	sentinel := errors.New("access denied")
	err := StartSession(context.Background(), &stubSession{err: sentinel}, &captureRunner{}, "i-x", "r", "p", "e")
	require.ErrorIs(t, err, sentinel)
}

func TestStartSession_PluginRunError(t *testing.T) {
	s := &stubSession{out: &ssm.StartSessionOutput{
		SessionId: aws.String("s"), StreamUrl: aws.String("u"), TokenValue: aws.String("t"),
	}}
	r := &captureRunner{err: errors.New("plugin exit 2")}

	err := StartSession(context.Background(), s, r, "i-x", "r", "p", "e")
	require.Error(t, err)
	require.Contains(t, err.Error(), "plugin exit 2")
}

func TestSSMEndpoint(t *testing.T) {
	require.Equal(t, "https://ssm.us-east-1.amazonaws.com", SSMEndpoint("us-east-1"))
	require.Equal(t, "https://ssm.eu-west-2.amazonaws.com", SSMEndpoint("eu-west-2"))
}
