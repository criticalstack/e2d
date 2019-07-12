package aws

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/aws/ec2metadata"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/pkg/errors"

	"github.com/criticalstack/e2d/pkg/log"
)

// NewConfig returns a new aws.Config with default values.
func NewConfig() (aws.Config, error) {
	cfg, err := external.LoadDefaultAWSConfig()
	if err != nil {
		return cfg, errors.Errorf("cannot load default aws config: %v", err)
	}
	metadata := ec2metadata.New(cfg)
	if cfg.Region == "" {
		cfg.Region, err = metadata.Region()
		if err != nil {
			return cfg, errors.Errorf("cannot determine AWS region: %v", err)
		}
	}
	return cfg, nil
}

// getRoleNameFromInstanceMetadata returns the IAM role related to the IAM
// instance profile attached to the current instance. It relies on the
// ec2metadata service to perform the IAM instance profile lookup, and the IAM
// service to find the related IAM role.
func getRoleNameFromInstanceMetadata(cfg aws.Config) (string, error) {
	info, err := ec2metadata.New(cfg).IAMInfo()
	if err != nil {
		return "", errors.Wrap(err, "cannot retrieve IAM info from metadata")
	}
	if info.InstanceProfileArn == "" {
		return "", errors.Wrap(err, "IAM instance profile not attached")
	}
	log.Debugf("instanceProfileArn: %#v", info.InstanceProfileArn)

	// Parse out the instance profile name
	parsedArn, err := arn.Parse(info.InstanceProfileArn)
	if err != nil {
		return "", errors.Wrapf(err, "cannot parse ARN: %#v", info.InstanceProfileArn)
	}
	instanceProfileName := strings.Replace(parsedArn.Resource, "instance-profile/", "", 1)
	log.Debugf("instanceProfileName: %#v", instanceProfileName)

	// Determine the IAM role associated with the instance profile
	return newIAMClient(cfg).getInstanceProfile(context.Background(), instanceProfileName)
}

// NewConfigWithAssumedRole returns a new aws.Config with default values, and
// assumes the IAM role for the attached IAM instance profile for the current
// instance using the provided session name.
func NewConfigWithAssumedRole(name string) (aws.Config, error) {
	cfg, err := NewConfig()
	if err != nil {
		return cfg, err
	}
	arn, err := getRoleNameFromInstanceMetadata(cfg)
	if err != nil {
		return cfg, err
	}
	return AssumeRoleSession(cfg, arn, name)
}
