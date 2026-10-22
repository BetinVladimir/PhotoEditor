package semanticreview

import (
	"bytes"
	"testing"
)

func removalParamsFixture() RemovalPreviewParams {
	return RemovalPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "remove_object", ModelID: "big_lama", ContextPixels: 128, FeatherPixels: 8}
}

func TestRemovalPreviewParamsCanonicalRoundTrip(t *testing.T) {
	p := removalParamsFixture()
	raw, sha, err := FreezeRemovalPreviewParams(p)
	if err != nil || len(sha) != 64 {
		t.Fatal("freeze failed", err)
	}
	result, err := ParseRemovalPreviewParams(raw)
	if err != nil || result != p {
		t.Fatal("round trip failed", err)
	}
	for _, invalid := range [][]byte{append(append([]byte{}, raw...), ' '), bytes.Replace(raw, []byte(`"context_pixels":128`), []byte(`"context_pixels":true`), 1), bytes.Replace(raw, []byte(`"kind":"remove_object"`), []byte(`"kind":"remove_object","confirmed":true`), 1)} {
		if _, err := ParseRemovalPreviewParams(invalid); err == nil {
			t.Fatal("accepted invalid document")
		}
	}
}

func TestRemovalPreviewParamsRejectUnsupportedControls(t *testing.T) {
	for _, mutate := range []func(*RemovalPreviewParams){
		func(p *RemovalPreviewParams) { p.Kind = "local_exposure" },
		func(p *RemovalPreviewParams) { p.ModelID = "sd15_inpaint" },
		func(p *RemovalPreviewParams) { p.ContextPixels = 31 },
		func(p *RemovalPreviewParams) { p.FeatherPixels = 65 },
		func(p *RemovalPreviewParams) { p.Protection = p.Candidate },
	} {
		p := removalParamsFixture()
		mutate(&p)
		if body, sha, err := FreezeRemovalPreviewParams(p); err == nil || body != nil || sha != "" {
			t.Fatal("invalid proposal frozen")
		}
	}
}
