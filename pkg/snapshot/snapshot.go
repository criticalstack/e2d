package snapshot

import (
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

type Snapshotter interface {
	Load() (io.ReadCloser, error)
	Save(io.ReadCloser) error
}

var schemes = []string{
	"file://",
	"s3://",
	"http://",
	"https://",
}

func hasValidScheme(url string) bool {
	for _, s := range schemes {
		if strings.HasPrefix(url, s) {
			return true
		}
	}
	return false
}

type Type int

const (
	FileType Type = iota
	S3Type
	SpacesType
)

type URL struct {
	Type   Type
	Bucket string
	Path   string
}

var (
	ErrInvalidScheme  = errors.New("invalid scheme")
	ErrCannotParseURL = errors.New("cannot parse url")
)

// ParseSnapshotBackupURL deconstructs a uri into a type prefix and a bucket
// example inputs and outputs:
//   file://file                                -> file://, file
//   s3://bucket                                -> s3://, bucket
//   https://nyc3.digitaloceanspaces.com/bucket -> digitaloceanspaces, bucket
func ParseSnapshotBackupURL(s string) (*URL, error) {
	if !hasValidScheme(s) {
		return nil, errors.Wrapf(ErrInvalidScheme, "url does not specify valid scheme: %#v", s)
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}

	switch strings.ToLower(u.Scheme) {
	case "file":
		return &URL{
			Type: FileType,
			Path: filepath.Join(u.Host, u.Path),
		}, nil
	case "s3":
		if u.Path == "" {
			u.Path = "etcd.snapshot"
		}
		return &URL{
			Type:   S3Type,
			Bucket: u.Host,
			Path:   strings.TrimPrefix(u.Path, "/"),
		}, nil
	case "http", "https":
		if strings.Contains(u.Host, "digitaloceanspaces") {
			bucket, path := parseBucketKey(strings.TrimPrefix(u.Path, "/"))
			return &URL{
				Type:   SpacesType,
				Bucket: bucket,
				Path:   path,
			}, nil
		}
	}
	return nil, errors.Wrap(ErrCannotParseURL, s)
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
