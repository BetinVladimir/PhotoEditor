package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func TestPortraitDetailMaterialDerivesExactMasks(t *testing.T) {
	original, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(original, &images) != nil || len(images) == 0 {
		t.Fatal("image fixture")
	}
	imageRaw, err := canonical.Bytes(images[0])
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(body []byte) v2input.Ref {
		digest := sha256.Sum256(body)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
	}
	imageRef := ref(imageRaw)
	mask := func(protected bool) review.MaskRef {
		return review.MaskRef{Artifact: review.ArtifactRef{
			ID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Canvas: image.Canvas, TransformChain: image.TransformChain,
			Selector: "manual", Confidence: 1, Protected: protected}
	}
	region, guard := mask(false), mask(true)
	guardRaw, err := canonical.Marshal(guard)
	if err != nil {
		t.Fatal(err)
	}
	guardRef := ref(guardRaw)
	detailModule := map[string]any{"enabled": true,
		"teeth_strength": 0.4, "shine_strength": 0.2,
		"under_eye_strength": 0.3, "glasses_policy": "review"}
	resolvedBody := map[string]any{
		"params": map[string]any{"image": imageRef},
		"mode":   "fixed",
		"modules": map[string]any{"extensions": map[string]any{
			"portrait_detail": detailModule}},
	}
	resolvedHash, err := canonical.Hash(resolvedBody)
	if err != nil {
		t.Fatal(err)
	}
	resolvedBody["hash"] = resolvedHash
	resolvedRaw, err := canonical.Marshal(resolvedBody)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRef := ref(resolvedRaw)
	paramsRaw, err := canonical.Marshal(map[string]any{
		"image": imageRef, "resolved": resolvedRef,
		"regions": []any{map[string]any{
			"region_id": "eyes", "kind": "sclera", "mask": region,
			"confirmed": true, "strength": 0.5}},
		"protected": []any{guard},
	})
	if err != nil {
		t.Fatal(err)
	}
	paramsRef := ref(paramsRaw)
	source := PortraitDetailProducerCandidate{
		Image: imageRef, Params: paramsRef, Resolved: resolvedRef}
	masks, err := ValidatePortraitDetailProducerMaterial(source, guardRef,
		imageRaw, paramsRaw, resolvedRaw, guardRaw)
	if err != nil || len(masks) != 2 {
		t.Fatal("valid detail material refused", err)
	}
	inventory := map[uuid.UUID]bool{}
	for _, entry := range masks {
		inventory[entry.ArtifactID] = true
	}
	if !inventory[region.Artifact.ID] || !inventory[guard.Artifact.ID] {
		t.Fatal("material omitted an actual mask")
	}
	changed := append([]byte(nil), paramsRaw...)
	changed[len(changed)-1] = ' '
	if _, err = ValidatePortraitDetailProducerMaterial(source, guardRef,
		imageRaw, changed, resolvedRaw, guardRaw); err == nil {
		t.Fatal("changed Params bytes admitted")
	}
	changedResolved := map[string]any{
		"params": map[string]any{"image": imageRef}, "mode": "skip",
		"modules": map[string]any{"extensions": map[string]any{
			"portrait_detail": detailModule}},
	}
	changedHash, _ := canonical.Hash(changedResolved)
	changedResolved["hash"] = changedHash
	changedRaw, _ := canonical.Marshal(changedResolved)
	changedSource := source
	changedSource.Resolved = ref(changedRaw)
	changedParamsRaw, err := canonical.Marshal(map[string]any{
		"image": imageRef, "resolved": changedSource.Resolved,
		"regions": []any{map[string]any{
			"region_id": "eyes", "kind": "sclera", "mask": region,
			"confirmed": true, "strength": 0.5}},
		"protected": []any{guard},
	})
	if err != nil {
		t.Fatal(err)
	}
	changedSource.Params = ref(changedParamsRaw)
	if _, err = ValidatePortraitDetailProducerMaterial(changedSource, guardRef,
		imageRaw, changedParamsRaw, changedRaw, guardRaw); err == nil {
		t.Fatal("disabled detail material admitted")
	}
	malformed := map[string]any{"params": map[string]any{"image": imageRef},
		"mode": "fixed", "modules": map[string]any{"extensions": map[string]any{
			"portrait_detail": map[string]any{"enabled": true,
				"teeth_strength": 1.1, "shine_strength": 0.2,
				"under_eye_strength": 0.3, "glasses_policy": "review"}}}}
	malformedHash, err := canonical.Hash(malformed)
	if err != nil {
		t.Fatal(err)
	}
	malformed["hash"] = malformedHash
	malformedRaw, err := canonical.Marshal(malformed)
	if err != nil {
		t.Fatal(err)
	}
	badSource := source
	badSource.Resolved = ref(malformedRaw)
	badParams, err := canonical.Marshal(map[string]any{
		"image": imageRef, "resolved": badSource.Resolved,
		"regions": []any{map[string]any{
			"region_id": "eyes", "kind": "sclera", "mask": region,
			"confirmed": true, "strength": 0.5}},
		"protected": []any{guard},
	})
	if err != nil {
		t.Fatal(err)
	}
	badSource.Params = ref(badParams)
	if _, err = ValidatePortraitDetailProducerMaterial(badSource, guardRef,
		imageRaw, badParams, malformedRaw, guardRaw); err == nil {
		t.Fatal("invalid resolved detail strength admitted")
	}
}
