package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestLocalTaskPlanBindsOperationParamsAndProfile(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var sources []json.RawMessage
	if json.Unmarshal(raw, &sources) != nil {
		t.Fatal("fixture")
	}
	sourceRaw := sources[0]
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(sourceRaw)
	canvas, _ := canonical.Hash(source.Canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}
	mask := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	guard := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	cost, _ := LocalPortraitResources(source.Canvas.Width, source.Canvas.Height)
	hair, _, _ := (HairParams{SchemaVersion: 1, Base: binding, Mask: mask, Protected: guard, Strength: 1, SaturationFactor: 1, Detail: .5}).Compile()
	clothing, _, _ := (ClothingParams{SchemaVersion: 1, Base: binding, Mask: mask, Protected: guard, Strength: 1, SaturationFactor: 1, WrinkleStrength: .5}).Compile()
	for operation, params := range map[string][]byte{"portrait.hair": hair, "portrait.clothing": clothing} {
		plan, e := CompileLocalTaskPlan(context.Background(), operation, binding, sourceRaw, params, strings.Repeat("a", 64), cost)
		if e != nil || plan.Operation != operation || plan.ReservedBytes != cost.OutputBytes || len(plan.Profile) == 0 {
			t.Fatal(plan, e)
		}
		hash := sha256.Sum256(plan.Params)
		if hex.EncodeToString(hash[:]) != plan.ParamsSHA {
			t.Fatal("Params digest mismatch")
		}
		other := "portrait.hair"
		if operation == other {
			other = "portrait.clothing"
		}
		bad, e := CompileLocalTaskPlan(context.Background(), other, binding, sourceRaw, params, strings.Repeat("a", 64), cost)
		if e == nil || bad.Params != nil || bad.Profile != nil {
			t.Fatal("wrong operation accepted")
		}
	}
	var object map[string]any
	_ = json.Unmarshal(hair, &object)
	object["unknown"] = true
	malformed, _ := canonical.Marshal(object)
	bad, e := CompileLocalTaskPlan(context.Background(), "portrait.hair", binding, sourceRaw, malformed, strings.Repeat("a", 64), cost)
	if e == nil || bad.Params != nil {
		t.Fatal("extra Params accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bad, e = CompileLocalTaskPlan(ctx, "portrait.hair", binding, sourceRaw, hair, strings.Repeat("a", 64), cost)
	if !errors.Is(e, context.Canceled) || bad.Params != nil || bad.Profile != nil {
		t.Fatal("cancelled plan escaped")
	}
}
