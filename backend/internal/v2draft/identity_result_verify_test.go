package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type identityResultCatalogFixture struct {
	items  map[string]objectstore.Object
	reads  map[string]int
	rotate bool
}

func (c *identityResultCatalogFixture) ResolveIdentityResultObject(_ context.Context,
	_ IdentityResultScope, role string, ref v2input.Ref) (objectstore.Object, error) {
	obj, ok := c.items[role]
	if !ok || obj.SHA256 != ref.SHA256 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	c.reads[role]++
	if c.rotate && role == "pixels" && c.reads[role] > 2 && c.reads[role]%2 == 0 {
		obj.VersionID = "rotated"
	}
	return obj, nil
}

func TestIdentityPublishedResultChecksOutputTrioAndVersions(t *testing.T) {
	input, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(input, &images) != nil || len(images) == 0 {
		t.Fatal("working image fixture")
	}
	afterRaw := images[0]
	after, err := review.ParseWorkingImage(afterRaw)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(body []byte) v2input.Ref {
		sum := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	afterRef := ref(afterRaw)
	canvasHash, err := canonical.Hash(after.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	params := v2input.IdentityCompareParams{SchemaVersion: 1,
		Before:        v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: canvasHash},
		After:         v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: afterRef, CanvasHash: canvasHash},
		EffectiveMask: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)},
		Protected:     v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)},
		ModelLockSHA:  strings.Repeat("d", 64),
		Calibration:   v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)},
		Consents:      v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}}
	paramsRaw, _, err := v2input.CompileIdentityCompareParams(params)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]float32, int(after.Canvas.Width*after.Canvas.Height))
	pixels := restorationArrayFixture("(300, 400)", values)
	pixelRef := ref(pixels)
	lock := params.ModelLockSHA
	maskRaw, err := canonical.Marshal(review.MaskRef{Artifact: review.ArtifactRef{ID: pixelRef.ArtifactID, SHA256: pixelRef.SHA256},
		Canvas: after.Canvas, TransformChain: after.TransformChain, Selector: "identity_changed",
		Confidence: 1, ModelLockID: &lock, Protected: false})
	if err != nil {
		t.Fatal(err)
	}
	maskRef := ref(maskRaw)
	reportRaw, err := canonical.Marshal(map[string]any{"schema_version": 1, "status": "not_measurable",
		"faces": []any{}, "changed_mask": maskRef, "generated_mask": nil,
		"outside_mask_max_error": 0, "calibration": params.Calibration})
	if err != nil {
		t.Fatal(err)
	}
	reportRef := ref(reportRaw)
	scope := IdentityResultScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New(), Root: reportRef}
	catalog := &identityResultCatalogFixture{items: map[string]objectstore.Object{}, reads: map[string]int{}}
	objects := restorationArrayReaderFixture{}
	for _, item := range []struct {
		role  string
		ref   v2input.Ref
		body  []byte
		media string
	}{
		{"report", reportRef, reportRaw, "application/json"},
		{"mask", maskRef, maskRaw, "application/json"},
		{"pixels", pixelRef, pixels, "application/x-npy"},
	} {
		catalog.items[item.role] = objectstore.Object{Key: item.role, VersionID: "v1", SHA256: item.ref.SHA256, Bytes: int64(len(item.body)), ContentType: item.media}
		objects[item.role] = item.body
	}
	got, err := VerifyIdentityPublishedResult(context.Background(), scope, catalog, objects, paramsRaw, afterRaw)
	if err != nil || got.Report != reportRef || got.Mask != maskRef || got.Pixels != pixelRef || got.Status != "not_measurable" {
		t.Fatal(got, err)
	}
	catalog.rotate = true
	if _, err := VerifyIdentityPublishedResult(context.Background(), scope, catalog, objects, paramsRaw, afterRaw); err == nil {
		t.Fatal("rotated pixels accepted")
	}
	catalog.rotate = false
	objects["pixels"] = append([]byte(nil), pixels...)
	objects["pixels"][len(pixels)-1] ^= 1
	if _, err := VerifyIdentityPublishedResult(context.Background(), scope, catalog, objects, paramsRaw, afterRaw); err == nil {
		t.Fatal("corrupt NPY accepted")
	}
}
