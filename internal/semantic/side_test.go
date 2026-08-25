package semantic

import "testing"

func TestExportSidesCompatible(t *testing.T) {
	cases := []struct {
		caller, provider string
		want             bool
	}{
		{"client", "client", true},
		{"client", "shared", true},
		{"client", "server", false},
		{"server", "server", true},
		{"server", "shared", true},
		{"server", "client", false},
		{"shared", "shared", true},
		{"shared", "client", false},
		{"shared", "server", false},
		{"unknown", "shared", false},
		{"client", "unknown", false},
	}
	for _, tc := range cases {
		if got := ExportSidesCompatible(tc.caller, tc.provider); got != tc.want {
			t.Fatalf("ExportSidesCompatible(%q,%q)=%v want %v", tc.caller, tc.provider, got, tc.want)
		}
	}
}
