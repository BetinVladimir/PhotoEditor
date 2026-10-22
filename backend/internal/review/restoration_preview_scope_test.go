package review

import "testing"

func TestRestorationPreviewUsesOnlyCandidateReviewKinds(t *testing.T) {
	for _, kind := range []string{"render.compose", "optical.negative", "portrait.glare", "portrait.open_eyes", "image.reflection", "image.relight"} {
		if !candidatePreviewKind(kind) {
			t.Fatal("review derivative kind refused", kind)
		}
	}
	for _, kind := range []string{"", "defects", "sensor_dust", "media.preview"} {
		if candidatePreviewKind(kind) {
			t.Fatal("unqualified review derivative kind accepted", kind)
		}
	}
}

func TestRestorationPreviewNodeAndFallbackScope(t *testing.T) {
	for _, kind := range []string{"portrait.glare", "image.reflection"} {
		for _, side := range []string{"before", "after"} {
			for _, variant := range []string{"preview1600", "preview2560"} {
				want := "restoration." + side + ":" + variant
				if got := candidatePreviewNodeKey(kind, side, variant); got != want {
					t.Fatalf("%s %s %s: node=%s, want %s", kind, side, variant, got, want)
				}
				if candidatePreviewAssetFallbackAllowed(kind, side) {
					t.Fatalf("restoration %s preview must remain unavailable without its receipt", side)
				}
			}
		}
	}
	if got := candidatePreviewNodeKey("render.compose", "after", "preview1600"); got != "render.derivative:preview1600" {
		t.Fatal("render preview node identity changed", got)
	}
	if !candidatePreviewAssetFallbackAllowed("render.compose", "before") || candidatePreviewAssetFallbackAllowed("render.compose", "after") {
		t.Fatal("existing render before-only asset fallback changed")
	}
}

func TestCandidatePreviewReceiptColumnsPreserveLegacySchema(t *testing.T) {
	for _, kind := range []string{"portrait.glare", "image.reflection"} {
		origin, receipt := candidatePreviewSQLColumns(kind)
		if origin != "r.v28_restoration_preview_task_id" ||
			receipt != "d.v28_restoration_preview_derivative_receipt" {
			t.Fatal("restoration receipt identity changed", kind)
		}
	}
	origin, receipt := candidatePreviewSQLColumns("render.compose")
	if origin != "r.v26_derivative_task_id" || receipt != "d.v26_derivative_receipt" {
		t.Fatal("legacy preview receipt identity changed")
	}
}
