package aws

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/pkg/errors"

	"github.com/criticalstack/e2d/pkg/log"
)

func NewConfig() (*aws.Config, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}
	doc, err := ec2metadata.New(sess).GetInstanceIdentityDocument()
	if err != nil {
		return nil, err
	}
	cfg := &aws.Config{
		Region: aws.String(doc.Region),
	}
	log.Debugf("%#v", cfg)
	return cfg, nil
}

func getRoleNameFromInstanceMetadata(sess *session.Session) (string, error) {
	info, err := ec2metadata.New(sess).IAMInfo()
	if err != nil {
		return "", err
	}
	if info.InstanceProfileArn == "" {
		return "", errors.Wrap(err, "IAM instance profile not attached")
	}

	// Parse out the instance profile name
	parsedArn, err := arn.Parse(info.InstanceProfileArn)
	if err != nil {
		return "", errors.Wrapf(err, "cannot parse ARN: %#v", info.InstanceProfileArn)
	}
	instanceProfileName := strings.Replace(parsedArn.Resource, "instance-profile/", "", 1)

	// Determine the IAM role associated with the instance profile
	resp, err := iam.New(sess).GetInstanceProfileWithContext(context.TODO(), &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(instanceProfileName),
	})
	if err != nil {
		return "", err
	}
	if len(resp.InstanceProfile.Roles) > 1 {
		return "", errors.New("only 1 Role-InstanceProfile association is supported")
	}
	if resp.InstanceProfile != nil {
		for _, role := range resp.InstanceProfile.Roles {
			return aws.StringValue(role.Arn), err
		}
	}
	return "", errors.Errorf("cannot find instance profile: %v", instanceProfileName)
}

func NewConfigWithRoleSession(name string) (*aws.Config, error) {
	cfg, err := NewConfig()
	if err != nil {
		return nil, err
	}
	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, err
	}
	arn, err := getRoleNameFromInstanceMetadata(sess)
	if err != nil {
		return nil, err
	}
	p := &stscreds.AssumeRoleProvider{
		Client:          sts.New(sess),
		RoleSessionName: name,
		RoleARN:         arn,
		Duration:        15 * time.Minute,
	}
	_, err = p.Retrieve()
	if err != nil {
		if strings.Contains(err.Error(), "Access Denied") {
			cfg.Credentials = ec2rolecreds.NewCredentials(sess)
			return cfg, nil
		}
		return cfg, errors.Wrap(err, "assume AWS STS credentials")
	}
	cfg.Credentials = credentials.NewCredentials(p)
	return cfg, nil
}
