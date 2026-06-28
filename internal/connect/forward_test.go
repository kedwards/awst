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

func TestPortForward_Parameters(t *testing.T) {
	// No host → defaults to localhost (service on the instance itself).
	require.Equal(t, map[string][]string{
		"host": {"localhost"}, "portNumber": {"5432"}, "localPortNumber": {"5432"},
	}, PortForward{LocalPort: "5432", RemotePort: "5432"}.parameters())

	// Explicit host → remote target (e.g. RDS).
	require.Equal(t, map[string][]string{
		"host": {"db.internal"}, "portNumber": {"5432"}, "localPortNumber": {"15432"},
	}, PortForward{Host: "db.internal", LocalPort: "15432", RemotePort: "5432"}.parameters())
}

type recordSSM struct {
	in  *ssm.StartSessionInput
	out *ssm.StartSessionOutput
}

func (r *recordSSM) StartSession(_ context.Context, in *ssm.StartSessionInput, _ ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	r.in = in
	return r.out, nil
}

type recordRunner struct {
	args    []string
	logPath string
}

func (r *recordRunner) Run(args []string) error { r.args = args; return nil }

func (r *recordRunner) Start(args []string, logPath string) (int, error) {
	r.args = args
	r.logPath = logPath
	return 4242, nil
}

func TestStartPortForward_InstanceLocal(t *testing.T) {
	ssmStub := &recordSSM{out: &ssm.StartSessionOutput{
		SessionId: aws.String("s-1"), StreamUrl: aws.String("wss://x"), TokenValue: aws.String("tok"),
	}}
	runner := &recordRunner{}
	pf := PortForward{LocalPort: "15432", RemotePort: "5432"} // no host

	err := StartPortForward(context.Background(), ssmStub, runner, pf, "i-abc", "us-east-1", "dev", "https://ssm.us-east-1.amazonaws.com")
	require.NoError(t, err)

	// Always the RemoteHost document, host defaulting to localhost.
	require.Equal(t, docPortForward, aws.ToString(ssmStub.in.DocumentName))
	require.Equal(t, []string{"localhost"}, ssmStub.in.Parameters["host"])

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

func TestStartPortForward_RemoteHost(t *testing.T) {
	ssmStub := &recordSSM{out: &ssm.StartSessionOutput{SessionId: aws.String("s")}}
	runner := &recordRunner{}
	pf := PortForward{Host: "rds.internal", LocalPort: "5432", RemotePort: "5432"}

	err := StartPortForward(context.Background(), ssmStub, runner, pf, "i-abc", "us-west-2", "prod", "https://ssm.us-west-2.amazonaws.com")
	require.NoError(t, err)

	require.Equal(t, docPortForward, aws.ToString(ssmStub.in.DocumentName))
	require.Equal(t, []string{"rds.internal"}, ssmStub.in.Parameters["host"])
}

func TestStartPortForwardDetached(t *testing.T) {
	ssmStub := &recordSSM{out: &ssm.StartSessionOutput{
		SessionId: aws.String("s-1"), StreamUrl: aws.String("wss://x"), TokenValue: aws.String("tok"),
	}}
	runner := &recordRunner{}
	pf := PortForward{LocalPort: "15432", RemotePort: "5432"}

	pid, err := StartPortForwardDetached(context.Background(), ssmStub, runner, pf, "i-abc", "us-east-1", "dev", "https://ssm.us-east-1.amazonaws.com", "/tmp/forward-15432.log")
	require.NoError(t, err)
	require.Equal(t, 4242, pid)
	require.Equal(t, "/tmp/forward-15432.log", runner.logPath)

	// Same 6-arg shape the foreground path builds.
	require.Len(t, runner.args, 6)
	require.Equal(t, "us-east-1", runner.args[1])
	require.Equal(t, "StartSession", runner.args[2])
	require.Equal(t, "dev", runner.args[3])
}
