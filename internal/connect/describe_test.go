package connect

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/require"
)

type stubSSM struct {
	pages   []*ssm.DescribeInstanceInformationOutput
	pageN   int
	filters []ssmtypes.InstanceInformationStringFilter
	err     error
}

func (s *stubSSM) DescribeInstanceInformation(_ context.Context, in *ssm.DescribeInstanceInformationInput, _ ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.filters = in.Filters
	i := s.pageN
	s.pageN++
	if i >= len(s.pages) {
		return &ssm.DescribeInstanceInformationOutput{}, nil
	}
	return s.pages[i], nil
}

type stubEC2 struct {
	out *ec2.DescribeInstancesOutput
	err error

	calledWith []string
}

func (s *stubEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.calledWith = append(s.calledWith, in.InstanceIds...)
	return s.out, nil
}

func ec2Inst(id, name, state string) ec2types.Instance {
	inst := ec2types.Instance{
		InstanceId: aws.String(id),
		State:      &ec2types.InstanceState{Name: ec2types.InstanceStateName(state)},
	}
	if name != "" {
		inst.Tags = []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String(name)}}
	}
	return inst
}

func ssmInfo(id, ping string) ssmtypes.InstanceInformation {
	return ssmtypes.InstanceInformation{
		InstanceId: aws.String(id),
		PingStatus: ssmtypes.PingStatus(ping),
	}
}

func TestList_HappyPath_ReturnsCrossJoin(t *testing.T) {
	s := &stubSSM{
		pages: []*ssm.DescribeInstanceInformationOutput{
			{InstanceInformationList: []ssmtypes.InstanceInformation{
				ssmInfo("i-aaa", "Online"),
				ssmInfo("i-bbb", "Online"),
			}},
		},
	}
	e := &stubEC2{
		out: &ec2.DescribeInstancesOutput{
			Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{
				ec2Inst("i-aaa", "web-1", "running"),
				ec2Inst("i-bbb", "db-1", "running"),
			}}},
		},
	}

	got, err := List(context.Background(), s, e)
	require.NoError(t, err)
	require.Equal(t, []Instance{
		{ID: "i-bbb", Name: "db-1", State: "running", Ping: "Online"},
		{ID: "i-aaa", Name: "web-1", State: "running", Ping: "Online"},
	}, got)
	require.Equal(t, []string{"i-aaa", "i-bbb"}, e.calledWith)
}

func TestList_FiltersToOnlinePingStatus(t *testing.T) {
	s := &stubSSM{pages: []*ssm.DescribeInstanceInformationOutput{{}}}
	_, err := List(context.Background(), s, &stubEC2{})
	require.NoError(t, err)
	require.Len(t, s.filters, 1)
	require.Equal(t, "PingStatus", aws.ToString(s.filters[0].Key))
	require.Equal(t, []string{"Online"}, s.filters[0].Values)
}

func TestList_PaginatesSSMResults(t *testing.T) {
	s := &stubSSM{
		pages: []*ssm.DescribeInstanceInformationOutput{
			{
				InstanceInformationList: []ssmtypes.InstanceInformation{ssmInfo("i-aaa", "Online")},
				NextToken:               aws.String("p2"),
			},
			{
				InstanceInformationList: []ssmtypes.InstanceInformation{ssmInfo("i-bbb", "Online")},
			},
		},
	}
	e := &stubEC2{out: &ec2.DescribeInstancesOutput{
		Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{
			ec2Inst("i-aaa", "a", "running"),
			ec2Inst("i-bbb", "b", "running"),
		}}},
	}}

	got, err := List(context.Background(), s, e)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, 2, s.pageN)
}

func TestList_NoSSMInstances_NoEC2Call(t *testing.T) {
	s := &stubSSM{pages: []*ssm.DescribeInstanceInformationOutput{{}}}
	e := &stubEC2{}
	got, err := List(context.Background(), s, e)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Empty(t, e.calledWith, "should not call EC2 when SSM list is empty")
}

func TestList_SSMErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := List(context.Background(), &stubSSM{err: sentinel}, &stubEC2{})
	require.ErrorIs(t, err, sentinel)
}

func TestList_InstanceWithoutNameTag(t *testing.T) {
	s := &stubSSM{pages: []*ssm.DescribeInstanceInformationOutput{{
		InstanceInformationList: []ssmtypes.InstanceInformation{ssmInfo("i-untagged", "Online")},
	}}}
	e := &stubEC2{out: &ec2.DescribeInstancesOutput{
		Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{
			ec2Inst("i-untagged", "", "running"),
		}}},
	}}

	got, err := List(context.Background(), s, e)
	require.NoError(t, err)
	require.Equal(t, []Instance{{ID: "i-untagged", Name: "", State: "running", Ping: "Online"}}, got)
}

func TestResolve_EmptyArgReturnsAll(t *testing.T) {
	list := []Instance{
		{ID: "i-a", Name: "web"},
		{ID: "i-b", Name: "db"},
	}
	require.Equal(t, list, Resolve("", list))
}

func TestResolve_ByInstanceID(t *testing.T) {
	list := []Instance{
		{ID: "i-aaa", Name: "web"},
		{ID: "i-bbb", Name: "db"},
	}
	got := Resolve("i-bbb", list)
	require.Equal(t, []Instance{{ID: "i-bbb", Name: "db"}}, got)
}

func TestResolve_ByInstanceID_NoMatch(t *testing.T) {
	list := []Instance{{ID: "i-aaa", Name: "web"}}
	require.Empty(t, Resolve("i-ghost", list))
}

func TestResolve_BySubstring_CaseInsensitive(t *testing.T) {
	list := []Instance{
		{ID: "i-a", Name: "web-prod"},
		{ID: "i-b", Name: "db-prod"},
		{ID: "i-c", Name: "WEB-dev"},
	}
	got := Resolve("web", list)
	require.Equal(t, []Instance{
		{ID: "i-a", Name: "web-prod"},
		{ID: "i-c", Name: "WEB-dev"},
	}, got)
}

func TestResolve_NoMatchReturnsEmpty(t *testing.T) {
	list := []Instance{{ID: "i-a", Name: "web"}}
	require.Empty(t, Resolve("ghost", list))
}
