package releases

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.1.28", "0.1.27", 1},
		{"v0.1.27-gui", "0.1.28", -1},
		{"0.1.28", "0.1.28", 0},
	}

	for _, tc := range tests {
		got := CompareVersions(tc.a, tc.b)
		if got != tc.want {
			t.Fatalf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	if got := NormalizeVersion("v0.1.28"); got != "0.1.28" {
		t.Fatalf("NormalizeVersion = %q", got)
	}
}
