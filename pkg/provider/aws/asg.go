package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/pkg/errors"
)

type asgClient struct {
	*autoscaling.Client
}

func (c *asgClient) describeAutoScalingGroups(ctx context.Context, name string) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	req := c.DescribeAutoScalingGroupsRequest(&autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{name},
	})
	resp, err := req.Send(ctx)
	if err != nil {
		return nil, err
	}
	return resp.DescribeAutoScalingGroupsOutput, nil
}

func (c *asgClient) describeAutoScalingGroupsDesiredCapacity(ctx context.Context, name string) (int64, error) {
	resp, err := c.describeAutoScalingGroups(ctx, name)
	if err != nil {
		return 0, err
	}
	for _, group := range resp.AutoScalingGroups {
		return aws.Int64Value(group.DesiredCapacity), nil
	}
	return 0, errors.Errorf("cannot find autoscaling group: %#v", name)
}

func (c *asgClient) describeAutoScalingGroupsInstances(ctx context.Context, name string) ([]string, error) {
	resp, err := c.describeAutoScalingGroups(ctx, name)
	if err != nil {
		return nil, err
	}
	instances := make([]string, 0)
	for _, group := range resp.AutoScalingGroups {
		for _, instance := range group.Instances {
			instances = append(instances, aws.StringValue(instance.InstanceId))
		}
	}
	return instances, nil
}

func (c *asgClient) describeAutoScalingInstances(ctx context.Context, instanceID string) (string, error) {
	req := c.DescribeAutoScalingInstancesRequest(&autoscaling.DescribeAutoScalingInstancesInput{
		InstanceIds: []string{instanceID},
	})
	resp, err := req.Send(ctx)
	if err != nil {
		return "", err
	}
	for _, instance := range resp.AutoScalingInstances {
		return aws.StringValue(instance.AutoScalingGroupName), nil
	}
	return "", errors.Errorf("cannot find autoscaling group for instance: %#v", instanceID)
}
