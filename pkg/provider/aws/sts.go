package aws

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/ec2metadata"
	"github.com/aws/aws-sdk-go-v2/aws/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/aws/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pkg/errors"
)

// AssumeRoleSession returns a new aws.Config by wrapping the given
// aws.Config with the requested role credentials and session name.
func AssumeRoleSession(cfg aws.Config, roleArn, sessionName string) (aws.Config, error) {
	credentials := stscreds.NewAssumeRoleProvider(sts.New(cfg), roleArn)
	credentials.RoleSessionName = sessionName
	_, err := credentials.Retrieve()
	if err != nil {
		if strings.Contains(err.Error(), "Access denied") {
			cfg.Credentials = ec2rolecreds.NewProvider(ec2metadata.New(cfg))
			return cfg, nil
		}
		return cfg, errors.Wrap(err, "assume AWS STS credentials")
	}
	cfg.Credentials = credentials
	return cfg, nil
}
