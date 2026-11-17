package main

import "testing"

func TestNormalizeVersion(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{raw: "v0.1.5\n", want: "0.1.5"},
		{raw: "v0.1.5-3-gabcdef", want: "0.1.5-3-gabcdef"},
		{raw: "v0.1.5-dirty", want: "0.1.5-dirty"},
		{raw: "abcdef", want: "abcdef"},
		{raw: "", want: "dev"},
		{raw: "   ", want: "dev"},
		{raw: "bad version", want: "dev"},
	}
	for _, tc := range cases {
		if got := normalizeVersion(tc.raw); got != tc.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
