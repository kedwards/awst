package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/kedwards/aws-tools/internal/connect"
)

type stubSSM struct {
	infos     []ssmtypes.InstanceInformation
	startOut  *ssm.StartSessionOutput
	startCall *ssm.StartSessionInput
}

func (s *stubSSM) DescribeInstanceInformation(_ context.Context, _ *ssm.DescribeInstanceInformationInput, _ ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error) {
	return &ssm.DescribeInstanceInformationOutput{InstanceInformationList: s.infos}, nil
}
func (s *stubSSM) StartSession(_ context.Context, in *ssm.StartSessionInput, _ ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	s.startCall = in
	return s.startOut, nil
}

type stubEC2 struct {
	instances []ec2types.Instance
}

func (s *stubEC2) DescribeInstances(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return &ec2.DescribeInstancesOutput{
		Reservations: []ec2types.Reservation{{Instances: s.instances}},
	}, nil
}

type captureRunner struct {
	gotArgs []string
}

func (c *captureRunner) Run(args []string) error {
	c.gotArgs = args
	return nil
}

func ssmInfo(id string) ssmtypes.InstanceInformation {
	return ssmtypes.InstanceInformation{
		InstanceId: aws.String(id),
		PingStatus: ssmtypes.PingStatusOnline,
	}
}

func ec2Inst(id, name string) ec2types.Instance {
	return ec2types.Instance{
		InstanceId: aws.String(id),
		State:      &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
		Tags:       []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String(name)}},
	}
}

func connectTestDeps(ssm *stubSSM, ec2c *stubEC2, runner connect.PluginRunner, pluginErr error) connectDeps {
	return connectDeps{
		clients: func(ctx context.Context, profile, region string) (*ssmClients, error) {
			return &ssmClients{
				SSM:        ssm,
				EC2:        ec2c,
				SSMSession: ssm,
				Region:     "us-east-1",
				Profile:    profile,
			}, nil
		},
		runner:     runner,
		lookPlugin: func() error { return pluginErr },
	}
}

func runConnect(t *testing.T, d connectDeps, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newConnectCmd(d))
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func TestConnect_PluginMissing_ExitsWithHint(t *testing.T) {
	d := connectTestDeps(&stubSSM{}, &stubEC2{}, &captureRunner{}, errors.New("not found"))
	_, _, err := runConnect(t, d, "connect", "web")
	require.Error(t, err)
	require.Contains(t, err.Error(), "session-manager-plugin")
}

func TestConnect_NoArgs_ListsAndExits(t *testing.T) {
	ssmStub := &stubSSM{infos: []ssmtypes.InstanceInformation{ssmInfo("i-aaa"), ssmInfo("i-bbb")}}
	ec2Stub := &stubEC2{instances: []ec2types.Instance{ec2Inst("i-aaa", "web"), ec2Inst("i-bbb", "db")}}
	d := connectTestDeps(ssmStub, ec2Stub, &captureRunner{}, nil)

	out, _, err := runConnect(t, d, "connect")
	require.Error(t, err, "no-arg should exit non-zero")
	require.Contains(t, out, "web")
	require.Contains(t, out, "i-aaa")
	require.Contains(t, out, "db")
	require.Contains(t, out, "i-bbb")
}

func TestConnect_NoMatch_ListsAndExits(t *testing.T) {
	ssmStub := &stubSSM{infos: []ssmtypes.InstanceInformation{ssmInfo("i-aaa")}}
	ec2Stub := &stubEC2{instances: []ec2types.Instance{ec2Inst("i-aaa", "web")}}
	d := connectTestDeps(ssmStub, ec2Stub, &captureRunner{}, nil)

	out, _, err := runConnect(t, d, "connect", "ghost")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no instance matched")
	require.Contains(t, out, "web", "should still list available instances")
}

func TestConnect_AmbiguousMatch_ListsAndExits(t *testing.T) {
	ssmStub := &stubSSM{infos: []ssmtypes.InstanceInformation{ssmInfo("i-aaa"), ssmInfo("i-bbb")}}
	ec2Stub := &stubEC2{instances: []ec2types.Instance{ec2Inst("i-aaa", "web-1"), ec2Inst("i-bbb", "web-2")}}
	d := connectTestDeps(ssmStub, ec2Stub, &captureRunner{}, nil)

	out, _, err := runConnect(t, d, "connect", "web")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous")
	require.Contains(t, out, "web-1")
	require.Contains(t, out, "web-2")
}

func TestConnect_ExactID_StartsSession(t *testing.T) {
	ssmStub := &stubSSM{
		infos: []ssmtypes.InstanceInformation{ssmInfo("i-aaa")},
		startOut: &ssm.StartSessionOutput{
			SessionId: aws.String("s1"), StreamUrl: aws.String("wss://x"), TokenValue: aws.String("t"),
		},
	}
	ec2Stub := &stubEC2{instances: []ec2types.Instance{ec2Inst("i-aaa", "web")}}
	runner := &captureRunner{}
	d := connectTestDeps(ssmStub, ec2Stub, runner, nil)

	_, _, err := runConnect(t, d, "connect", "i-aaa")
	require.NoError(t, err)
	require.Equal(t, "i-aaa", aws.ToString(ssmStub.startCall.Target))
	require.Len(t, runner.gotArgs, 6, "plugin should be invoked with 6 args")
	require.Equal(t, "us-east-1", runner.gotArgs[1])
}

func TestConnect_SingleNameMatch_StartsSession(t *testing.T) {
	ssmStub := &stubSSM{
		infos: []ssmtypes.InstanceInformation{ssmInfo("i-aaa"), ssmInfo("i-bbb")},
		startOut: &ssm.StartSessionOutput{
			SessionId: aws.String("s1"), StreamUrl: aws.String("wss://x"), TokenValue: aws.String("t"),
		},
	}
	ec2Stub := &stubEC2{instances: []ec2types.Instance{ec2Inst("i-aaa", "web"), ec2Inst("i-bbb", "db")}}
	runner := &captureRunner{}
	d := connectTestDeps(ssmStub, ec2Stub, runner, nil)

	_, _, err := runConnect(t, d, "connect", "web")
	require.NoError(t, err)
	require.Equal(t, "i-aaa", aws.ToString(ssmStub.startCall.Target))
}

type errSSM struct{ err error }

func (e *errSSM) DescribeInstanceInformation(context.Context, *ssm.DescribeInstanceInformationInput, ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error) {
	return nil, e.err
}
func (e *errSSM) StartSession(context.Context, *ssm.StartSessionInput, ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	return nil, e.err
}

func TestConnect_AuthFailure_HintsAtLogin(t *testing.T) {
	ssmStub := &errSSM{err: errors.New("operation error SSM: DescribeInstanceInformation, no valid SSO token found in cache")}
	d := connectDeps{
		clients: func(_ context.Context, profile, region string) (*ssmClients, error) {
			return &ssmClients{SSM: ssmStub, EC2: &stubEC2{}, SSMSession: ssmStub, Region: "us-east-1", Profile: profile}, nil
		},
		runner:     &captureRunner{},
		lookPlugin: func() error { return nil },
	}
	_, _, err := runConnect(t, d, "connect", "-p", "dev", "web")
	require.Error(t, err)
	require.Contains(t, err.Error(), "awst login dev")
}

func TestConnect_HelpFlag(t *testing.T) {
	d := connectTestDeps(&stubSSM{}, &stubEC2{}, &captureRunner{}, nil)
	out, _, err := runConnect(t, d, "connect", "-h")
	require.NoError(t, err)
	require.Contains(t, out, "connect [instance]")
	require.Contains(t, out, "--profile")
	require.Contains(t, out, "--region")
}
