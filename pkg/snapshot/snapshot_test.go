package snapshot

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

func TestParseSnapshotBackupURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expected    *URL
		expectedErr error
	}{
		{
			name:        "empty",
			url:         "",
			expected:    nil,
			expectedErr: ErrInvalidScheme,
		},
		{
			name:     "file (empty)",
			url:      "file://",
			expected: &URL{Type: FileType},
		},
		{
			name:     "file",
			url:      "file://abc",
			expected: &URL{Type: FileType, Path: "abc"},
		},
		{
			name:     "file",
			url:      "file://abc/snapshot.gz",
			expected: &URL{Type: FileType, Path: "abc/snapshot.gz"},
		},
		{
			name:     "file",
			url:      "file:///abc",
			expected: &URL{Type: FileType, Path: "/abc"},
		},
		{
			name:     "s3",
			url:      "s3://abc",
			expected: &URL{Type: S3Type, Bucket: "abc"},
		},
		{
			name:     "spaces",
			url:      "https://nyc3.digitaloceanspaces.com/abc",
			expected: &URL{Type: SpacesType, Bucket: "abc", Path: "etcd.snapshot"},
		},
		{
			name:     "spaces",
			url:      "https://nyc3.digitaloceanspaces.com/abc/snapshot.gz",
			expected: &URL{Type: SpacesType, Bucket: "abc", Path: "snapshot.gz"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := ParseSnapshotBackupURL(tt.url)
			if err != nil && errors.Cause(err) != tt.expectedErr {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.expected, u); diff != "" {
				t.Errorf("snapshot: after Parse differs: (-want +got)\n%s", diff)
			}
		})
	}
}
