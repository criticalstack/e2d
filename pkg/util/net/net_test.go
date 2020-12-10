package net

import "testing"

func TestIsRoutableIPv4(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{
			"",
			false,
		},
		{
			"0.0.0.0",
			false,
		},
		{
			"127.0.0.1",
			false,
		},
		{
			"10.100.100.100",
			true,
		},
	}
	for _, tt := range tests {
		if got := IsRoutableIPv4(tt.s); got != tt.want {
			t.Errorf("IsRoutableIPv4(%s) = %v, want %v", tt.s, got, tt.want)
		}
	}
}
