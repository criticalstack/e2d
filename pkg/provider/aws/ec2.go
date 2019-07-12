package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/pkg/errors"
)

type ec2Client struct {
	*ec2.Client
}

func (c *ec2Client) describeTags(ctx context.Context, instanceID string) (map[string]string, error) {
	req := c.DescribeTagsRequest(&ec2.DescribeTagsInput{
		Filters: []ec2.Filter{
			{
				Name:   aws.String("resource-id"),
				Values: []string{instanceID},
			},
		},
	})
	resp, err := req.Send(ctx)
	if err != nil {
		return nil, err
	}
	tags := make(map[string]string)
	for _, tag := range resp.Tags {
		tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
	}
	return tags, nil
}

func (c *ec2Client) describeInstance(ctx context.Context, instanceID string) (*ec2.Instance, error) {
	req := c.DescribeInstancesRequest(&ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	})
	resp, err := req.Send(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range resp.Reservations {
		for _, instance := range r.Instances {
			return &instance, nil
		}
	}
	return nil, errors.Errorf("instance not found: %v", instanceID)
}

func (c *ec2Client) describeInstanceIPAddress(ctx context.Context, instanceID string) (string, error) {
	instance, err := c.describeInstance(ctx, instanceID)
	if err != nil {
		return "", err
	}
	return aws.StringValue(instance.PrivateIpAddress), nil
}
