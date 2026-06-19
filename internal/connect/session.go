package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// SSMSessionClient is the slice of *ssm.Client used to start a session.
type SSMSessionClient interface {
	StartSession(ctx context.Context, in *ssm.StartSessionInput, optFns ...func(*ssm.Options)) (*ssm.StartSessionOutput, error)
}

// PluginRunner exec'd session-manager-plugin with the args the AWS CLI uses
// internally. Kept as an interface so tests can record argv without forking.
type PluginRunner interface {
	Run(args []string) error
}

// PluginName is the binary name the AWS CLI invokes. Override via
// AWST_SSM_PLUGIN env var if you have a non-PATH install.
const PluginName = "session-manager-plugin"

// StartSession calls ssm:StartSession and hands the response off to runner
// with the 6-arg shape the AWS CLI passes to session-manager-plugin.
func StartSession(ctx context.Context, s SSMSessionClient, runner PluginRunner, instanceID, region, profile, endpoint string) error {
	in := &ssm.StartSessionInput{Target: aws.String(instanceID)}
	out, err := s.StartSession(ctx, in)
	if err != nil {
		return fmt.Errorf("start ssm session: %w", err)
	}
	return dispatchToPlugin(runner, out, in, region, profile, endpoint)
}

// dispatchToPlugin builds the 6-arg session-manager-plugin invocation that
// the AWS CLI uses: the StartSession response JSON, region, the literal
// "StartSession" op, profile, the request-parameters JSON, and endpoint.
// The params JSON mirrors whatever fields were set on the request (Target
// for a shell, plus DocumentName/Parameters for port forwarding).
func dispatchToPlugin(runner PluginRunner, out *ssm.StartSessionOutput, in *ssm.StartSessionInput, region, profile, endpoint string) error {
	respJSON, err := json.Marshal(map[string]string{
		"SessionId":  aws.ToString(out.SessionId),
		"StreamUrl":  aws.ToString(out.StreamUrl),
		"TokenValue": aws.ToString(out.TokenValue),
	})
	if err != nil {
		return fmt.Errorf("marshal session response: %w", err)
	}
	params := map[string]any{"Target": aws.ToString(in.Target)}
	if in.DocumentName != nil {
		params["DocumentName"] = aws.ToString(in.DocumentName)
		params["Parameters"] = in.Parameters
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal session params: %w", err)
	}
	return runner.Run([]string{
		string(respJSON),
		region,
		"StartSession",
		profile,
		string(paramsJSON),
		endpoint,
	})
}

// SSMEndpoint returns the public-partition SSM endpoint for region.
// ponytail: aws/govcloud/china have different DNS; upgrade path is to ask
// the SDK's endpoint resolver. Public partition is enough for slice 3.
func SSMEndpoint(region string) string {
	return fmt.Sprintf("https://ssm.%s.amazonaws.com", region)
}

// ExecRunner runs session-manager-plugin via os/exec, wiring the user's
// terminal so Ctrl+C terminates the session cleanly.
type ExecRunner struct {
	Binary string // optional override; defaults to PluginName
}

func (e ExecRunner) Run(args []string) error {
	bin := e.Binary
	if bin == "" {
		bin = PluginName
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
