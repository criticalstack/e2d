package snapshot

import (
	"reflect"
	"testing"
)

func TestParseSnapshotBackupURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "empty",
			in:   "",
			want: []string{"", ""},
		},
		{
			name: "file (empty)",
			in:   "file://",
			want: []string{"file://", ""},
		},
		{
			name: "file",
			in:   "file://abc",
			want: []string{"file://", "abc"},
		},
		{
			name: "file",
			in:   "file:///abc",
			want: []string{"file://", "/abc"},
		},
		{
			name: "s3",
			in:   "s3://abc",
			want: []string{"s3://", "abc"},
		},
		{
			name: "spaces",
			in:   "https://nyc3.digitaloceanspaces.com/abc",
			want: []string{"digitaloceanspaces", "abc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if pref, stem := ParseSnapshotBackupURL(tt.in); !reflect.DeepEqual(pref, tt.want[0]) || !reflect.DeepEqual(stem, tt.want[1]) {
				t.Errorf("parseSnapshotStore() = %v, %v, want %+v", pref, stem, tt.want)
			}
		})
	}
}
