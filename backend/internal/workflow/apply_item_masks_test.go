package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"reflect"
	"strings"
	"testing"
)

func operatorMaskTwoSources(t *testing.T) ([]applyInput, []applyItemMaskInput) {
	t.Helper()
	items := []applyInput{}
	inputs := []applyItemMaskInput{}
	for index := 0; index < 2; index++ {
		image := review.WorkingImage{Canvas: review.Canvas{Width: int64(20 + index), Height: int64(30 + index), SourceSHA256: strings.Repeat(string(rune('a'+index)), 64)}, TransformChain: json.RawMessage(`{"steps":[]}`)}
		source := applyInput{ItemID: uuid.New(), VersionID: uuid.New(), Image: styleArtifact{ArtifactID: uuid.New(), SHA256: strings.Repeat(string(rune('c'+index)), 64)}, Working: image}
		target := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("f", 64)}, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1}
		guard := target
		guard.Artifact.ID = uuid.New()
		guard.Protected = true
		bindings := []applyOverrideMaskBinding{{BindingID: "target", Purpose: "inpaint_target", Mask: target, Confirmed: true, Strength: 1}, {BindingID: "guard", Purpose: "protected", Mask: guard, Strength: 1}}
		body, err := canonical.Marshal(map[string]any{"schema_version": 1, "revision": 1, "input_sha256": source.Image.SHA256, "modules": map[string]any{"strength": 1}, "mask_refs": bindings})
		if err != nil {
			t.Fatal(err)
		}
		pin, err := canonical.Hash(json.RawMessage(body))
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, source)
		inputs = append(inputs, applyItemMaskInput{ItemID: source.ItemID, AssetVersionID: source.VersionID, InputSHA: source.Image.SHA256, BodySHA: pin, Body: body})
	}
	return items, inputs
}
func TestApplyItemMasksTwoSourcesStayIsolatedAndReplayExactly(t *testing.T) {
	items, inputs := operatorMaskTwoSources(t)
	bound, err := boundApplyItemMasks(items, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(bound) != 2 || bound[items[0].ItemID][0].Mask.Canvas == bound[items[1].ItemID][0].Mask.Canvas || bound[items[0].ItemID][0].Mask.Artifact == bound[items[1].ItemID][0].Mask.Artifact {
		t.Fatal("distinct source masks merged")
	}
	replay, err := boundApplyItemMasks(items, inputs)
	if err != nil || !reflect.DeepEqual(bound, replay) {
		t.Fatal("same immutable source replay changed")
	}
	for index := range items {
		if bound[items[index].ItemID][0].Mask.Canvas != items[index].Working.Canvas || !bound[items[index].ItemID][1].Mask.Protected {
			t.Fatal("source/protection changed")
		}
	}
}
func TestApplyItemMasksRejectSourceSubstitutionAndIncompleteScope(t *testing.T) {
	for _, kind := range []string{"version", "source", "body", "item", "missing", "duplicate", "geometry"} {
		t.Run(kind, func(t *testing.T) {
			items, inputs := operatorMaskTwoSources(t)
			switch kind {
			case "version":
				inputs[0].AssetVersionID = items[1].VersionID
			case "source":
				inputs[0].InputSHA = items[1].Image.SHA256
			case "body":
				inputs[0].Body = inputs[1].Body
			case "item":
				inputs[0].ItemID = uuid.New()
			case "missing":
				inputs = inputs[:1]
			case "duplicate":
				inputs[1] = inputs[0]
			case "geometry":
				items[0].Working.Canvas.Width++
			}
			if _, err := boundApplyItemMasks(items, inputs); err == nil {
				t.Fatal("cross-source or incomplete masks accepted")
			}
		})
	}
}
func TestOperatorDocumentBudgetRefusesBeforeIO(t *testing.T) {
	ledger := &OperatorApplyLedger{objects: map[uuid.UUID]styleArtifact{uuid.New(): {Bytes: 8}}}
	if operatorObjectBudget(ledger, 10, 2) != nil {
		t.Fatal("exact remaining budget refused")
	}
	if operatorObjectBudget(ledger, 10, 3) == nil {
		t.Fatal("excess retained bytes accepted")
	}
}
