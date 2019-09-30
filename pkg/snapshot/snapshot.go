package snapshot

import (
	"io"
	"strings"
)

const (
	FileProviderType   = "file://"
	S3ProviderType     = "s3://"
	SpacesProviderType = "digitaloceanspaces"
)

var providerTypes = []string{
	FileProviderType,
	S3ProviderType,
	SpacesProviderType,
}

type Snapshotter interface {
	Load() (io.ReadCloser, error)
	Save(io.ReadCloser) error
}

// ParseSnapshotBackupURL deconstructs a uri into a type prefix and a bucket
// example inputs and outputs:
//   file://file                                -> file://, file
//   s3://bucket                                -> s3://, bucket
//   https://nyc3.digitaloceanspaces.com/bucket -> digitaloceanspaces, bucket
func ParseSnapshotBackupURL(url string) (string, string) {
	match := ""
	for _, t := range providerTypes {
		if strings.Contains(url, t) {
			match = t
			break
		}
	}
	switch match {
	case FileProviderType:
		fallthrough
	case S3ProviderType:
		prefIndex := strings.Index(url, "://")
		if prefIndex < 0 {
			return "", url
		}
		return url[:prefIndex+len("://")], url[prefIndex+len("://"):]
	case SpacesProviderType:
		return SpacesProviderType, url[strings.LastIndex(url, "/")+1:]
	}
	return "", ""
}

func parseBucketKey(s string) (string, string) {
	parts := strings.SplitN(s, "/", 2)
	switch len(parts) {
	case 1:
		return parts[0], "etcd.snapshot"
	case 2:
		return parts[0], parts[1]
	default:
		return "", ""
	}
}
