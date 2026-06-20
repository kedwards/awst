// Package ssmexec sends a shell command across one or more SSM-managed
// instances via ssm:SendCommand and aggregates the per-target results.
// Named ssmexec (not exec) so it doesn't clash with the stdlib os/exec.
package ssmexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/kedwards/awst/v3/internal/connect"
)

const (
	documentName    = "AWS-RunShellScript"
	defaultTimeout  = "600"
	pollInterval    = 2 * time.Second
	executionParam  = "executionTimeout"
	commandsParam   = "commands"
)

// CmdClient is the slice of *ssm.Client used by Run.
type CmdClient interface {
	SendCommand(ctx context.Context, in *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error)
	GetCommandInvocation(ctx context.Context, in *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error)
}

// Result is the per-instance outcome of a Run call.
type Result struct {
	InstanceID string
	Status     string
	Stdout     string
	Stderr     string
	ExitCode   int32
}

// Failed reports whether the result represents a non-success terminal state.
func (r Result) Failed() bool {
	return r.Status != string(ssmtypes.CommandInvocationStatusSuccess)
}

// Expand splits patterns on comma, resolves each piece against list via
// connect.Resolve, and returns the union (deduped, original order). A
// pattern that matches nothing is a hard error — silent skips invite the
// "ran on fewer instances than I thought" footgun.
func Expand(patterns string, list []connect.Instance) ([]connect.Instance, error) {
	pieces := strings.Split(patterns, ",")
	var out []connect.Instance
	seen := map[string]bool{}
	matched := false
	for _, p := range pieces {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		matched = true
		hits := connect.Resolve(p, list)
		if len(hits) == 0 {
			return nil, fmt.Errorf("no instance matched %q", p)
		}
		for _, h := range hits {
			if seen[h.ID] {
				continue
			}
			seen[h.ID] = true
			out = append(out, h)
		}
	}
	if !matched {
		return nil, errors.New("no instance patterns supplied")
	}
	return out, nil
}

// Run sends command to instanceIDs as a single SendCommand invocation,
// polls GetCommandInvocation per instance until every target reaches a
// terminal status, and returns one Result per instance in input order.
func Run(ctx context.Context, c CmdClient, command string, instanceIDs []string, sleep func(time.Duration)) ([]Result, error) {
	if len(instanceIDs) == 0 {
		return nil, errors.New("no instances to run on")
	}
	if sleep == nil {
		sleep = time.Sleep
	}

	send, err := c.SendCommand(ctx, &ssm.SendCommandInput{
		InstanceIds:  instanceIDs,
		DocumentName: aws.String(documentName),
		Parameters: map[string][]string{
			commandsParam:  {command},
			executionParam: {defaultTimeout},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("ssm send-command: %w", err)
	}
	if send.Command == nil || send.Command.CommandId == nil {
		return nil, errors.New("ssm send-command: missing command id in response")
	}
	cmdID := send.Command.CommandId

	results := make(map[string]*Result, len(instanceIDs))
	for _, id := range instanceIDs {
		results[id] = &Result{InstanceID: id}
	}

	remaining := append([]string(nil), instanceIDs...)
	for len(remaining) > 0 {
		// Sleep before every poll, including the first — SSM propagates
		// SendCommand → GetCommandInvocation asynchronously, so the first
		// poll right after SendCommand routinely races and 400s.
		sleep(pollInterval)

		stillPending := remaining[:0]
		for _, id := range remaining {
			out, err := c.GetCommandInvocation(ctx, &ssm.GetCommandInvocationInput{
				CommandId:  cmdID,
				InstanceId: aws.String(id),
			})
			if err != nil {
				// SSM propagates SendCommand → GetCommandInvocation asynchronously;
				// the first few polls after SendCommand routinely return
				// InvocationDoesNotExist before the invocation record exists.
				// Treat it as "still pending" rather than fatal.
				var dne *ssmtypes.InvocationDoesNotExist
				if errors.As(err, &dne) {
					stillPending = append(stillPending, id)
					continue
				}
				return nil, fmt.Errorf("ssm get-command-invocation %s: %w", id, err)
			}
			if !terminal(out.Status) {
				stillPending = append(stillPending, id)
				continue
			}
			r := results[id]
			r.Status = string(out.Status)
			r.Stdout = aws.ToString(out.StandardOutputContent)
			r.Stderr = aws.ToString(out.StandardErrorContent)
			r.ExitCode = out.ResponseCode
		}
		remaining = stillPending
	}

	out := make([]Result, 0, len(instanceIDs))
	for _, id := range instanceIDs {
		out = append(out, *results[id])
	}
	return out, nil
}

func terminal(s ssmtypes.CommandInvocationStatus) bool {
	switch s {
	case ssmtypes.CommandInvocationStatusPending,
		ssmtypes.CommandInvocationStatusInProgress,
		ssmtypes.CommandInvocationStatusDelayed,
		ssmtypes.CommandInvocationStatusCancelling:
		return false
	}
	return true
}
