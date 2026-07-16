package main

import "testing"

func TestParseHostRoute(t *testing.T) {
	tests := []struct {
		dest string
		want string
	}{
		{"2001:db8::42", "2001:db8::42"},
		{"2001:db8::42/128", "2001:db8::42"},
		{"2001:db8::/96", ""},
		{"fe80::1", "fe80::1"},
		{"fe80::1/128", "fe80::1"},
		{"", ""},
		{"192.168.1.1", ""},
		{"192.168.1.1/32", ""},
	}

	for _, tc := range tests {
		if got := parseHostRoute(tc.dest); got != tc.want {
			t.Errorf("parseHostRoute(%q) = %q, want %q", tc.dest, got, tc.want)
		}
	}
}
