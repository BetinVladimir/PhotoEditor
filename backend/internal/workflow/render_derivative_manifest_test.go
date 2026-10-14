package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/review"
)

func TestRenderManifestBindsSourceGeometryAndProfile(t *testing.T) {
	ref := func() review.ArtifactRef { return review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	source, output, manifest := ref(), ref(), ref()
	image := review.WorkingImage{DType: "float32", Canvas: review.Canvas{Width: 3200, Height: 5}}
	profile, err := media.ProfileHash("preview1600", strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, alpha := range []bool{false, true} {
		image.Alpha = nil
		mime := "image/jpeg"
		if alpha {
			a := ref()
			image.Alpha = &a
			mime = "image/webp"
		}
		m := RenderDerivativeManifest{Source: source, Derivative: output, Variant: "preview1600", ProfileHash: profile, Width: 1600, Height: 2, MediaType: mime, ColorProfile: "srgb", MetadataPolicy: "stripped", Alpha: alpha}
		body, err := canonical.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		manifest.SHA256 = renderDerivativeBytesSHA(body)
		if _, err = ParseRenderDerivativeManifest(body, manifest, source, output, image, "preview1600", strings.Repeat("b", 64)); err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err = json.Unmarshal(body, &fields); err != nil {
			t.Fatal(err)
		}
		for field, value := range map[string]any{"height": 3, "width": 3200, "variant": "preview2560", "profile_hash": strings.Repeat("c", 64), "alpha": !alpha, "media_type": "image/png", "metadata_policy": "preserved", "unexpected": true} {
			bad := map[string]any{}
			for k, v := range fields {
				bad[k] = v
			}
			bad[field] = value
			raw, _ := canonical.Marshal(bad)
			manifest.SHA256 = renderDerivativeBytesSHA(raw)
			if _, err = ParseRenderDerivativeManifest(raw, manifest, source, output, image, "preview1600", strings.Repeat("b", 64)); err == nil {
				t.Fatalf("accepted substitution %s", field)
			}
		}
		delete(fields, "alpha")
		raw, _ := canonical.Marshal(fields)
		manifest.SHA256 = renderDerivativeBytesSHA(raw)
		if _, err = ParseRenderDerivativeManifest(raw, manifest, source, output, image, "preview1600", strings.Repeat("b", 64)); err == nil {
			t.Fatal("missing explicit alpha accepted")
		}
	}
}
