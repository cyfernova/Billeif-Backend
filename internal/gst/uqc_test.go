package gst

import "testing"

func TestNormalizeProductUQC(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "blank defaults to pcs", raw: "", want: "PCS"},
		{name: "lowercase valid code", raw: "kgs", want: "KGS"},
		{name: "unknown becomes oth", raw: "widgets", want: "OTH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeProductUQC(tt.raw); got != tt.want {
				t.Fatalf("NormalizeProductUQC(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCanonicalSnapshotUQC(t *testing.T) {
	if got := CanonicalSnapshotUQC("", "pcs"); got != "PCS" {
		t.Fatalf("CanonicalSnapshotUQC fallback = %q, want PCS", got)
	}
	if got := CanonicalSnapshotUQC("", "widgets"); got != "OTH" {
		t.Fatalf("CanonicalSnapshotUQC invalid fallback = %q, want OTH", got)
	}
}
