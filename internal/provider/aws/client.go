package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/pkg/errors"

	netutil "github.com/criticalstack/e2d/pkg/util/net"
)

type Client struct {
	*autoscaling.AutoScaling
	*ec2.EC2
	*ec2metadata.EC2Metadata
}

func NewClient(cfg *aws.Config) (*Client, error) {
	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, err
	}
	c := &Client{
		AutoScaling: autoscaling.New(sess),
		EC2:         ec2.New(sess),
		EC2Metadata: ec2metadata.New(sess),
	}
	return c, nil
}

func (c *Client) getGroupName(ctx context.Context, instanceID string) (string, error) {
	resp, err := c.DescribeAutoScalingInstancesWithContext(ctx, &autoscaling.DescribeAutoScalingInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
	if err != nil {
		return "", err
	}
	for _, instance := range resp.AutoScalingInstances {
		return aws.StringValue(instance.AutoScalingGroupName), nil
	}
	return "", errors.Errorf("cannot find autoscaling group for instance: %#v", instanceID)
}

func (c *Client) getInstanceIPAddress(ctx context.Context, instanceID string) (string, error) {
	resp, err := c.DescribeInstancesWithContext(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	})
	if err != nil {
		return "", err
	}
	for _, r := range resp.Reservations {
		for _, instance := range r.Instances {
			return aws.StringValue(instance.PrivateIpAddress), nil
		}
	}
	return "", errors.Errorf("instance not found: %v", instanceID)
}

func (c *Client) GetAutoScalingGroupAddresses(ctx context.Context) ([]string, error) {
	doc, err := c.GetInstanceIdentityDocument()
	if err != nil {
		return nil, err
	}
	groupName, err := c.getGroupName(ctx, doc.InstanceID)
	if err != nil {
		return nil, err
	}
	instances := make([]string, 0)
	err = c.DescribeAutoScalingGroupsPagesWithContext(ctx, &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{groupName}),
	}, func(page *autoscaling.DescribeAutoScalingGroupsOutput, lastPage bool) bool {
		for _, group := range page.AutoScalingGroups {
			for _, instance := range group.Instances {
				instances = append(instances, aws.StringValue(instance.InstanceId))
			}
		}
		return !lastPage
	})
	if err != nil {
		return nil, err
	}
	addrs := make([]string, 0)
	for _, i := range instances {
		if i == doc.InstanceID {
			continue
		}
		addr, err := c.getInstanceIPAddress(ctx, i)
		if err != nil {
			return nil, err
		}
		if !netutil.IsRoutableIPv4(addr) {
			continue
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

func (c *Client) GetAddressesByTag(ctx context.Context, kvs map[string]string) ([]string, error) {
	doc, err := c.GetInstanceIdentityDocument()
	if err != nil {
		return nil, err
	}
	filters := make([]*ec2.Filter, 0)
	for k, v := range kvs {
		filters = append(filters, &ec2.Filter{
			Name:   aws.String(fmt.Sprintf("tag:%s", k)),
			Values: aws.StringSlice([]string{v}),
		})
	}
	instances := make([]string, 0)
	err = c.EC2.DescribeTagsPagesWithContext(ctx, &ec2.DescribeTagsInput{
		Filters: filters,
	}, func(page *ec2.DescribeTagsOutput, lastPage bool) bool {
		for _, tag := range page.Tags {
			if aws.StringValue(tag.ResourceType) != ec2.ResourceTypeInstance {
				continue
			}
			instances = append(instances, aws.StringValue(tag.ResourceId))
		}
		return !lastPage
	})
	if err != nil {
		return nil, err
	}
	addrs := make([]string, 0)
	for _, i := range instances {
		if i == doc.InstanceID {
			continue
		}
		addr, err := c.getInstanceIPAddress(ctx, i)
		if err != nil {
			return nil, err
		}
		if !netutil.IsRoutableIPv4(addr) {
			continue
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}
