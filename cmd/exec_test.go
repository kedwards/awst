package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type execStubSSM struct {
	infos   []ssmtypes.InstanceInformation
	sendErr error
	cmdID   string
	getOuts map[string]*ssm.GetCommandInvocationOutput
}

func (s *execStubSSM) DescribeInstanceInformation(_ context.Context, _ *ssm.DescribeInstanceInformationInput, _ ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error) {
	return &ssm.DescribeInstanceInformationOutput{InstanceInformationList: s.infos}, nil
}
func (s *execStubSSM) StartSession(_ context.Context, _ *ssm.StartSessionInput, _ ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	return nil, errors.New("not used in exec tests")
}
func (s *execStubSSM) SendCommand(_ context.Context, _ *ssm.SendCommandInput, _ ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
	if s.sendErr != nil {
		return nil, s.sendErr
	}
	return &ssm.SendCommandOutput{Command: &ssmtypes.Command{CommandId: aws.String(s.cmdID)}}, nil
}
func (s *execStubSSM) GetCommandInvocation(_ context.Context, in *ssm.GetCommandInvocationInput, _ ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
	out, ok := s.getOuts[aws.ToString(in.InstanceId)]
	if !ok {
		return nil, errors.New("no stub for " + aws.ToString(in.InstanceId))
	}
	return out, nil
}

type execStubEC2 struct {
	instances []ec2types.Instance
}

func (s *execStubEC2) DescribeInstances(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return &ec2.DescribeInstancesOutput{
		Reservations: []ec2types.Reservation{{Instances: s.instances}},
	}, nil
}

func execTestDeps(ssmStub *execStubSSM, ec2Stub *execStubEC2) execDeps {
	return execDeps{
		clients: func(_ context.Context, profile, _ string) (*ssmClients, error) {
			return &ssmClients{
				SSM:        ssmStub,
				EC2:        ec2Stub,
				SSMSession: ssmStub,
				Cmd:        ssmStub,
				Region:     "us-east-1",
				Profile:    profile,
			}, nil
		},
		sleep: func(time.Duration) {},
	}
}

func runExec(t *testing.T, d execDeps, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := &cobra.Command{Use: "awst", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newExecCmd(d))
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

func invOut(status ssmtypes.CommandInvocationStatus, stdout, stderr string, code int32) *ssm.GetCommandInvocationOutput {
	return &ssm.GetCommandInvocationOutput{
		Status:                status,
		StandardOutputContent: aws.String(stdout),
		StandardErrorContent:  aws.String(stderr),
		ResponseCode:          code,
	}
}

func TestExec_HappyPath_MultiInstance(t *testing.T) {
	ssmStub := &execStubSSM{
		infos: []ssmtypes.InstanceInformation{ssmInfo("i-aaa"), ssmInfo("i-bbb")},
		cmdID: "cmd-1",
		getOuts: map[string]*ssm.GetCommandInvocationOutput{
			"i-aaa": invOut(ssmtypes.CommandInvocationStatusSuccess, "hello-a\n", "", 0),
			"i-bbb": invOut(ssmtypes.CommandInvocationStatusSuccess, "hello-b\n", "", 0),
		},
	}
	ec2Stub := &execStubEC2{instances: []ec2types.Instance{ec2Inst("i-aaa", "web-1"), ec2Inst("i-bbb", "web-2")}}
	d := execTestDeps(ssmStub, ec2Stub)

	out, _, err := runExec(t, d, "exec", "-c", "echo hello", "-i", "web")
	require.NoError(t, err)
	require.Contains(t, out, "i-aaa")
	require.Contains(t, out, "i-bbb")
	require.Contains(t, out, "hello-a")
	require.Contains(t, out, "hello-b")
	require.Contains(t, out, "Success")
}

func TestExec_FailingInstance_ExitsNonZero(t *testing.T) {
	ssmStub := &execStubSSM{
		infos: []ssmtypes.InstanceInformation{ssmInfo("i-aaa"), ssmInfo("i-bbb")},
		cmdID: "cmd-1",
		getOuts: map[string]*ssm.GetCommandInvocationOutput{
			"i-aaa": invOut(ssmtypes.CommandInvocationStatusSuccess, "ok\n", "", 0),
			"i-bbb": invOut(ssmtypes.CommandInvocationStatusFailed, "", "boom\n", 1),
		},
	}
	ec2Stub := &execStubEC2{instances: []ec2types.Instance{ec2Inst("i-aaa", "w"), ec2Inst("i-bbb", "d")}}
	d := execTestDeps(ssmStub, ec2Stub)

	_, _, err := runExec(t, d, "exec", "-c", "x", "-i", "w,d")
	require.Error(t, err)
	require.Contains(t, err.Error(), "i-bbb")
}

func TestExec_MissingCommand(t *testing.T) {
	d := execTestDeps(&execStubSSM{}, &execStubEC2{})
	_, _, err := runExec(t, d, "exec", "-i", "web")
	require.Error(t, err)
	require.Contains(t, err.Error(), "command")
}

func TestExec_MissingInstances(t *testing.T) {
	d := execTestDeps(&execStubSSM{}, &execStubEC2{})
	_, _, err := runExec(t, d, "exec", "-c", "echo x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "instance")
}

func TestExec_NoMatchingInstances(t *testing.T) {
	ssmStub := &execStubSSM{infos: []ssmtypes.InstanceInformation{ssmInfo("i-aaa")}}
	ec2Stub := &execStubEC2{instances: []ec2types.Instance{ec2Inst("i-aaa", "web")}}
	d := execTestDeps(ssmStub, ec2Stub)

	_, _, err := runExec(t, d, "exec", "-c", "x", "-i", "ghost")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
}

func TestExec_HelpFlag(t *testing.T) {
	d := execTestDeps(&execStubSSM{}, &execStubEC2{})
	out, _, err := runExec(t, d, "exec", "-h")
	require.NoError(t, err)
	require.Contains(t, out, "exec")
	require.Contains(t, out, "--command")
	require.Contains(t, out, "--instances")
}

func TestExec_AuthFailure_HintsAtLogin(t *testing.T) {
	ssmStub := &execStubSSM{}
	ssmStub.sendErr = errors.New("no valid SSO token found in cache")
	// Pre-seed describe output so we reach SendCommand.
	ssmStub.infos = []ssmtypes.InstanceInformation{ssmInfo("i-aaa")}
	ec2Stub := &execStubEC2{instances: []ec2types.Instance{ec2Inst("i-aaa", "web")}}
	d := execTestDeps(ssmStub, ec2Stub)

	_, _, err := runExec(t, d, "exec", "-p", "dev", "-c", "x", "-i", "web")
	require.Error(t, err)
	require.Contains(t, err.Error(), "awst login dev")
}
