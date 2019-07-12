package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/pkg/errors"
)

type iamClient struct {
	*iam.Client
}

func newIAMClient(cfg aws.Config) *iamClient {
	return &iamClient{iam.New(cfg)}
}

func (i *iamClient) getInstanceProfile(ctx context.Context, name string) (string, error) {
	req := i.GetInstanceProfileRequest(&iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(name),
	})
	resp, err := req.Send(ctx)
	if err != nil {
		return "", errors.Errorf("cannot get instance profile: %v", err)
	}
	if len(resp.InstanceProfile.Roles) > 1 {
		return "", errors.New("only 1 Role-InstanceProfile association is supported")
	}
	if resp.InstanceProfile != nil {
		for _, role := range resp.InstanceProfile.Roles {
			return aws.StringValue(role.Arn), err
		}
	}
	return "", errors.Errorf("cannot find instance profile: %v", name)
}
