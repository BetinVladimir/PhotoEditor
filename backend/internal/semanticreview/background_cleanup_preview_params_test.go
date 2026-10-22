package semanticreview

import (
	"bytes"
	"testing"
)

func backgroundCleanupParamsFixture() BackgroundCleanupPreviewParams {
	return BackgroundCleanupPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "background_cleanup", ModelID: "big_lama", ContextPixels: 128, FeatherPixels: 8}
}

func TestBackgroundCleanupPreviewParamsCanonicalRoundTrip(t *testing.T) {
	p := backgroundCleanupParamsFixture()
	raw, sha, err := FreezeBackgroundCleanupPreviewParams(p)
	if err != nil || len(sha) != 64 {
		t.Fatal("freeze failed", err)
	}
	result, err := ParseBackgroundCleanupPreviewParams(raw)
	if err != nil || result != p {
		t.Fatal("round trip failed", err)
	}
	for _, invalid := range [][]byte{append(append([]byte{}, raw...), ' '), bytes.Replace(raw, []byte(`"context_pixels":128`), []byte(`"context_pixels":true`), 1), bytes.Replace(raw, []byte(`"kind":"background_cleanup"`), []byte(`"kind":"background_cleanup","confirmed":true`), 1)} {
		if _, err := ParseBackgroundCleanupPreviewParams(invalid); err == nil {
			t.Fatal("accepted invalid document")
		}
	}
}

func TestBackgroundCleanupPreviewParamsRejectUnsupportedControls(t *testing.T) {
	for _, mutate := range []func(*BackgroundCleanupPreviewParams){
		func(p *BackgroundCleanupPreviewParams) { p.Kind = "local_exposure" },
		func(p *BackgroundCleanupPreviewParams) { p.Kind = "remove_object" },
		func(p *BackgroundCleanupPreviewParams) { p.Kind = "retouch_region" },
		func(p *BackgroundCleanupPreviewParams) { p.ModelID = "sd15_inpaint" },
		func(p *BackgroundCleanupPreviewParams) { p.ContextPixels = 31 },
		func(p *BackgroundCleanupPreviewParams) { p.FeatherPixels = 65 },
		func(p *BackgroundCleanupPreviewParams) { p.Protection = p.Candidate },
	} {
		p := backgroundCleanupParamsFixture()
		mutate(&p)
		if body, sha, err := FreezeBackgroundCleanupPreviewParams(p); err == nil || body != nil || sha != "" {
			t.Fatal("invalid proposal frozen")
		}
	}
}
