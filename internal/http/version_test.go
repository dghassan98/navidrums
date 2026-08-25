package httpapp

import "testing"

// TestAssetVersionIsNeverConstantInDev guards the point of cache busting: a
// build with no VCS stamp must not fall back to a fixed string, or browsers
// cache the stylesheet forever and CSS edits silently do nothing.
func TestAssetVersionIsNeverConstantInDev(t *testing.T) {
	v := AssetVersion()

	if v == "" {
		t.Fatal("AssetVersion is empty; the asset URL would have a bare ?v=")
	}
	if v == "unknown" {
		t.Error("AssetVersion is the constant \"unknown\", which never invalidates a cache")
	}
}
