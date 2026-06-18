package connect

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// SSMClient is the slice of *ssm.Client used by this package.
type SSMClient interface {
	DescribeInstanceInformation(ctx context.Context, in *ssm.DescribeInstanceInformationInput, optFns ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error)
}

// EC2Client is the slice of *ec2.Client used by this package.
type EC2Client interface {
	DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
}

// Instance is one SSM-managed EC2 instance enriched with its Name tag.
type Instance struct {
	ID    string
	Name  string
	State string
	Ping  string
}

// List returns every SSM-Online EC2 instance with its Name tag, sorted by
// Name then ID. Empty SSM result short-circuits the EC2 call.
func List(ctx context.Context, s SSMClient, e EC2Client) ([]Instance, error) {
	pingByID := map[string]string{}
	var ids []string
	var next *string
	for {
		out, err := s.DescribeInstanceInformation(ctx, &ssm.DescribeInstanceInformationInput{
			Filters: []ssmtypes.InstanceInformationStringFilter{
				{Key: aws.String("PingStatus"), Values: []string{"Online"}},
			},
			MaxResults: aws.Int32(50),
			NextToken:  next,
		})
		if err != nil {
			return nil, fmt.Errorf("describe ssm instances: %w", err)
		}
		for _, info := range out.InstanceInformationList {
			id := aws.ToString(info.InstanceId)
			ids = append(ids, id)
			pingByID[id] = string(info.PingStatus)
		}
		if out.NextToken == nil {
			break
		}
		next = out.NextToken
	}

	if len(ids) == 0 {
		return nil, nil
	}

	const batch = 100 // EC2 DescribeInstances InstanceIds limit
	out := make([]Instance, 0, len(ids))
	for i := 0; i < len(ids); i += batch {
		end := i + batch
		if end > len(ids) {
			end = len(ids)
		}
		resp, err := e.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: ids[i:end]})
		if err != nil {
			return nil, fmt.Errorf("describe ec2 instances: %w", err)
		}
		for _, r := range resp.Reservations {
			for _, inst := range r.Instances {
				id := aws.ToString(inst.InstanceId)
				name := ""
				for _, tag := range inst.Tags {
					if aws.ToString(tag.Key) == "Name" {
						name = aws.ToString(tag.Value)
						break
					}
				}
				state := ""
				if inst.State != nil {
					state = string(inst.State.Name)
				}
				out = append(out, Instance{
					ID:    id,
					Name:  name,
					State: state,
					Ping:  pingByID[id],
				})
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Resolve matches arg against the instance list.
// "" → returns all; "i-…" → exact ID; otherwise case-insensitive substring on Name.
func Resolve(arg string, instances []Instance) []Instance {
	if arg == "" {
		return instances
	}
	if strings.HasPrefix(arg, "i-") {
		for _, in := range instances {
			if in.ID == arg {
				return []Instance{in}
			}
		}
		return nil
	}
	needle := strings.ToLower(arg)
	var out []Instance
	for _, in := range instances {
		if strings.Contains(strings.ToLower(in.Name), needle) {
			out = append(out, in)
		}
	}
	return out
}
