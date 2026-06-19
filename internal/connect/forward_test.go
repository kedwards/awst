package connect

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/stretchr/testify/require"
)

func TestParseForwardSpec(t *testing.T) {
	t.Run("single port, local==remote", func(t *testing.T) {
		got, err := ParseForwardSpec("5432", "")
		require.NoError(t, err)
		require.Equal(t, []PortForward{{LocalPort: "5432", RemotePort: "5432"}}, got)
	})
	t.Run("local:remote", func(t *testing.T) {
		got, err := ParseForwardSpec("15432:5432", "")
		require.NoError(t, err)
		require.Equal(t, []PortForward{{LocalPort: "15432", RemotePort: "5432"}}, got)
	})
	t.Run("multi-port with host", func(t *testing.T) {
		got, err := ParseForwardSpec("8428,9093", "mon.internal")
		require.NoError(t, err)
		require.Equal(t, []PortForward{
			{Host: "mon.internal", LocalPort: "8428", RemotePort: "8428"},
			{Host: "mon.internal", LocalPort: "9093", RemotePort: "9093"},
		}, got)
	})
	t.Run("rejects non-numeric", func(t *testing.T) {
		_, err := ParseForwardSpec("abc", "")
		require.Error(t, err)
	})
	t.Run("rejects out of range", func(t *testing.T) {
		_, err := ParseForwardSpec("70000", "")
		require.Error(t, err)
	})
	t.Run("rejects empty", func(t *testing.T) {
		_, err := ParseForwardSpec("", "")
		require.Error(t, err)
	})
}

func TestPortForward_DocumentSelection(t *testing.T) {
	require.Equal(t, docPortForward, PortForward{LocalPort: "5432", RemotePort: "5432"}.document())
	require.Equal(t, docPortForwardRemote, PortForward{Host: "db.internal", LocalPort: "5432", RemotePort: "5432"}.document())
}

type recordSSM struct {
	in  *ssm.StartSessionInput
	out *ssm.StartSessionOutput
}

func (r *recordSSM) StartSession(_ context.Context, in *ssm.StartSessionInput, _ ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	r.in = in
	return r.out, nil
}

type recordRunner struct{ args []string }

func (r *recordRunner) Run(args []string) error { r.args = args; return nil }

func TestStartPortForward_LocalDoc(t *testing.T) {
	ssmStub := &recordSSM{out: &ssm.StartSessionOutput{
		SessionId: aws.String("s-1"), StreamUrl: aws.String("wss://x"), TokenValue: aws.String("tok"),
	}}
	runner := &recordRunner{}
	pf := PortForward{LocalPort: "15432", RemotePort: "5432"}

	err := StartPortForward(context.Background(), ssmStub, runner, pf, "i-abc", "us-east-1", "dev", "https://ssm.us-east-1.amazonaws.com")
	require.NoError(t, err)

	// API call uses the local-forwarding document, no host param.
	require.Equal(t, docPortForward, aws.ToString(ssmStub.in.DocumentName))
	require.Equal(t, map[string][]string{"portNumber": {"5432"}, "localPortNumber": {"15432"}}, ssmStub.in.Parameters)

	// Plugin gets the 6-arg shape; arg[4] carries DocumentName + Parameters.
	require.Len(t, runner.args, 6)
	require.Equal(t, "us-east-1", runner.args[1])
	require.Equal(t, "StartSession", runner.args[2])
	require.Equal(t, "dev", runner.args[3])
	var params map[string]any
	require.NoError(t, json.Unmarshal([]byte(runner.args[4]), &params))
	require.Equal(t, "i-abc", params["Target"])
	require.Equal(t, docPortForward, params["DocumentName"])
}

func TestStartPortForward_RemoteHostDoc(t *testing.T) {
	ssmStub := &recordSSM{out: &ssm.StartSessionOutput{SessionId: aws.String("s")}}
	runner := &recordRunner{}
	pf := PortForward{Host: "rds.internal", LocalPort: "5432", RemotePort: "5432"}

	err := StartPortForward(context.Background(), ssmStub, runner, pf, "i-abc", "us-west-2", "prod", "https://ssm.us-west-2.amazonaws.com")
	require.NoError(t, err)

	require.Equal(t, docPortForwardRemote, aws.ToString(ssmStub.in.DocumentName))
	require.Equal(t, []string{"rds.internal"}, ssmStub.in.Parameters["host"])
}
