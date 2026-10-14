package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

func TestPlannedManualReplayRetainsSourceAndRejectsInjectedFields(t *testing.T) {
	image := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	canvas := review.Canvas{Width: 40, Height: 30, Orientation: 1, SpaceID: "oriented:" + image.SHA256, SourceSHA256: image.SHA256}
	chain := json.RawMessage(`{"hash":"4430e7786edc0f8419f02e909c15422ebf572287a58132d8f6f33250ce053121","steps":[]}`)
	working := review.WorkingImage{Canvas: canvas, TransformChain: chain}
	vectors := map[string]any{"version": "manual_mask_v1", "source_sha256": image.SHA256, "canvas": canvas, "shapes": []any{map[string]any{"kind": "stroke", "sequence": 0, "mode": "add", "points": []any{map[string]any{"x": .5, "y": .5}}, "radius": .01, "hardness": 1}}}
	params := map[string]any{"image": image, "inline_vectors": vectors}
	raw, _ := canonical.Marshal(params)
	sha, _ := canonical.Hash(json.RawMessage(raw))
	if planned, err := verifyPlannedInteractiveManual(raw, sha, uuid.New(), image, working); !planned || err != nil {
		t.Fatal("planned manual replay rejected", err)
	}
	if _, err := verifyPlannedInteractiveManual(raw, strings.Repeat("f", 64), uuid.New(), image, working); err == nil {
		t.Fatal("changed saved params hash accepted")
	}
	params["extra"] = true
	raw, _ = canonical.Marshal(params)
	sha, _ = canonical.Hash(json.RawMessage(raw))
	if planned, err := verifyPlannedInteractiveManual(raw, sha, uuid.New(), image, working); !planned || err == nil {
		t.Fatal("injected field accepted")
	}
	delete(params, "extra")
	params["image"] = review.ArtifactRef{ID: uuid.New(), SHA256: image.SHA256}
	raw, _ = canonical.Marshal(params)
	sha, _ = canonical.Hash(json.RawMessage(raw))
	if planned, err := verifyPlannedInteractiveManual(raw, sha, uuid.New(), image, working); !planned || err == nil {
		t.Fatal("foreign image accepted")
	}
	template := json.RawMessage(`{"source_sha256":"a","shapes":[]}`)
	templateSHA, _ := canonical.Hash(template)
	if planned, err := verifyPlannedInteractiveManual(template, templateSHA, uuid.New(), image, working); planned || err != nil {
		t.Fatal("template classified as planned", err)
	}
}
