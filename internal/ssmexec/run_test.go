package ssmexec

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/require"

	"github.com/kedwards/awst/v3/internal/connect"
)

type stubCmd struct {
	sendOut *ssm.SendCommandOutput
	sendIn  *ssm.SendCommandInput
	sendErr error

	getOuts map[string][]*ssm.GetCommandInvocationOutput // keyed by instanceID, per-call queue
	getErrs map[string][]error                           // optional per-call error queue
	getN    map[string]int
}

func (s *stubCmd) SendCommand(_ context.Context, in *ssm.SendCommandInput, _ ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
	s.sendIn = in
	return s.sendOut, s.sendErr
}

func (s *stubCmd) GetCommandInvocation(_ context.Context, in *ssm.GetCommandInvocationInput, _ ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
	id := aws.ToString(in.InstanceId)
	if s.getN == nil {
		s.getN = map[string]int{}
	}
	idx := s.getN[id]
	s.getN[id]++
	if errs, ok := s.getErrs[id]; ok && idx < len(errs) && errs[idx] != nil {
		return nil, errs[idx]
	}
	q := s.getOuts[id]
	if idx >= len(q) {
		return nil, errors.New("stub exhausted for " + id)
	}
	return q[idx], nil
}

func okInv(status ssmtypes.CommandInvocationStatus, stdout, stderr string, code int32) *ssm.GetCommandInvocationOutput {
	return &ssm.GetCommandInvocationOutput{
		Status:                status,
		StandardOutputContent: aws.String(stdout),
		StandardErrorContent:  aws.String(stderr),
		ResponseCode:          code,
	}
}

type captureSleep struct{ calls []time.Duration }

func (c *captureSleep) sleep(d time.Duration) { c.calls = append(c.calls, d) }

func TestRun_HappyPath_SingleInstance(t *testing.T) {
	c := &stubCmd{
		sendOut: &ssm.SendCommandOutput{Command: &ssmtypes.Command{CommandId: aws.String("cmd-1")}},
		getOuts: map[string][]*ssm.GetCommandInvocationOutput{
			"i-aaa": {
				okInv(ssmtypes.CommandInvocationStatusInProgress, "", "", 0),
				okInv(ssmtypes.CommandInvocationStatusSuccess, "hello\n", "", 0),
			},
		},
	}
	sleep := &captureSleep{}
	results, err := Run(context.Background(), c, "echo hello", []string{"i-aaa"}, sleep.sleep)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "i-aaa", results[0].InstanceID)
	require.Equal(t, "Success", results[0].Status)
	require.Equal(t, "hello\n", results[0].Stdout)
	require.Equal(t, int32(0), results[0].ExitCode)

	require.Equal(t, []string{"i-aaa"}, c.sendIn.InstanceIds)
	require.Equal(t, "AWS-RunShellScript", aws.ToString(c.sendIn.DocumentName))
	require.Equal(t, []string{"echo hello"}, c.sendIn.Parameters["commands"])

	require.NotEmpty(t, sleep.calls, "should sleep between polls")
}

func TestRun_FailedInstance(t *testing.T) {
	c := &stubCmd{
		sendOut: &ssm.SendCommandOutput{Command: &ssmtypes.Command{CommandId: aws.String("cmd-1")}},
		getOuts: map[string][]*ssm.GetCommandInvocationOutput{
			"i-aaa": {okInv(ssmtypes.CommandInvocationStatusFailed, "", "boom\n", 1)},
		},
	}
	results, err := Run(context.Background(), c, "false", []string{"i-aaa"}, func(time.Duration) {})
	require.NoError(t, err, "Run itself succeeds even if a target fails")
	require.Equal(t, "Failed", results[0].Status)
	require.Equal(t, "boom\n", results[0].Stderr)
	require.Equal(t, int32(1), results[0].ExitCode)
}

func TestRun_MultiInstance_PollsBoth(t *testing.T) {
	c := &stubCmd{
		sendOut: &ssm.SendCommandOutput{Command: &ssmtypes.Command{CommandId: aws.String("cmd-1")}},
		getOuts: map[string][]*ssm.GetCommandInvocationOutput{
			"i-aaa": {
				okInv(ssmtypes.CommandInvocationStatusInProgress, "", "", 0),
				okInv(ssmtypes.CommandInvocationStatusSuccess, "a\n", "", 0),
			},
			"i-bbb": {
				okInv(ssmtypes.CommandInvocationStatusSuccess, "b\n", "", 0),
			},
		},
	}
	results, err := Run(context.Background(), c, "echo x", []string{"i-aaa", "i-bbb"}, func(time.Duration) {})
	require.NoError(t, err)
	require.Len(t, results, 2)
	byID := map[string]Result{}
	for _, r := range results {
		byID[r.InstanceID] = r
	}
	require.Equal(t, "a\n", byID["i-aaa"].Stdout)
	require.Equal(t, "b\n", byID["i-bbb"].Stdout)
}

func TestRun_SendCommandError(t *testing.T) {
	sentinel := errors.New("denied")
	c := &stubCmd{sendErr: sentinel}
	_, err := Run(context.Background(), c, "x", []string{"i-aaa"}, func(time.Duration) {})
	require.ErrorIs(t, err, sentinel)
}

func TestRun_RetriesOnInvocationDoesNotExist(t *testing.T) {
	// SSM commonly returns InvocationDoesNotExist for the first few polls
	// after SendCommand while the invocation record propagates.
	c := &stubCmd{
		sendOut: &ssm.SendCommandOutput{Command: &ssmtypes.Command{CommandId: aws.String("cmd-1")}},
		getErrs: map[string][]error{
			"i-aaa": {&ssmtypes.InvocationDoesNotExist{}, &ssmtypes.InvocationDoesNotExist{}, nil},
		},
		getOuts: map[string][]*ssm.GetCommandInvocationOutput{
			"i-aaa": {nil, nil, okInv(ssmtypes.CommandInvocationStatusSuccess, "ok\n", "", 0)},
		},
	}
	results, err := Run(context.Background(), c, "x", []string{"i-aaa"}, func(time.Duration) {})
	require.NoError(t, err)
	require.Equal(t, "Success", results[0].Status)
	require.Equal(t, "ok\n", results[0].Stdout)
}

func TestRun_EmptyInstanceList(t *testing.T) {
	_, err := Run(context.Background(), &stubCmd{}, "x", nil, func(time.Duration) {})
	require.Error(t, err)
}

func TestExpand_CommaSplitWithMix(t *testing.T) {
	list := []connect.Instance{
		{ID: "i-aaa", Name: "web-1"},
		{ID: "i-bbb", Name: "web-2"},
		{ID: "i-ccc", Name: "db-1"},
	}
	got, err := Expand("web, i-ccc", list)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"i-aaa", "i-bbb", "i-ccc"}, ids(got))
}

func TestExpand_DedupsAcrossPatterns(t *testing.T) {
	list := []connect.Instance{
		{ID: "i-aaa", Name: "web-1"},
		{ID: "i-bbb", Name: "web-2"},
	}
	got, err := Expand("web-1, web", list)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"i-aaa", "i-bbb"}, ids(got))
}

func TestExpand_NoMatchForPatternIsError(t *testing.T) {
	list := []connect.Instance{{ID: "i-aaa", Name: "web"}}
	_, err := Expand("ghost", list)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ghost")
}

func TestExpand_EmptyPatternsIsError(t *testing.T) {
	_, err := Expand("", []connect.Instance{{ID: "i-a", Name: "x"}})
	require.Error(t, err)
}

func ids(insts []connect.Instance) []string {
	out := make([]string, 0, len(insts))
	for _, i := range insts {
		out = append(out, i.ID)
	}
	return out
}
