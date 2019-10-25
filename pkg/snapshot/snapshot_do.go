package snapshot

import (
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
)

type DigitalOceanConfig struct {
	AccessToken     string
	SpacesURL       string
	SpacesAccessKey string
	SpacesSecretKey string
}

func parseSpacesURL(s string) (string, string, string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", "", "", err
	}
	bucket, key := parseBucketKey(strings.TrimPrefix(u.Path, "/"))
	return u.Host, bucket, key, nil
}

type DigitalOceanSnapshotter struct {
	*AmazonSnapshotter
}

func NewDigitalOceanSnapshotter(cfg *DigitalOceanConfig) (*DigitalOceanSnapshotter, error) {
	endpoint, spaceName, key, err := parseSpacesURL(cfg.SpacesURL)
	if err != nil {
		return nil, err
	}
	awsCfg := &aws.Config{
		Credentials: credentials.NewStaticCredentials(cfg.SpacesAccessKey, cfg.SpacesSecretKey, ""),
		Endpoint:    aws.String(endpoint),
		// This is counter intuitive, but it will fail with a non-AWS region name.
		Region: aws.String("us-east-1"),
	}
	s, err := newAmazonSnapshotter(awsCfg, spaceName, key)
	if err != nil {
		return nil, err
	}
	return &DigitalOceanSnapshotter{s}, nil
}
