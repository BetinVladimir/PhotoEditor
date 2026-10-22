package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredRecipeProtectionUsesExactOriginalOccurrence(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	makeSource := func(value any) AuthoredInstructionSource {
		raw, _ := canonical.Marshal(map[string]any{"recipe": map[string]any{"protected_mask": value}})
		return AuthoredInstructionSource{RecipeJCS: string(raw), OriginalRecipe: v2input.Ref{ArtifactID: uuid.New(), SHA256: authoredDetailSHA(raw)}}
	}
	source := makeSource(ref)
	got, err := authoredRecipeProtectionRef(source)
	if err != nil || got == nil || *got != ref {
		t.Fatalf("exact source lost: %v", err)
	}
	got.ArtifactID = uuid.New()
	again, _ := authoredRecipeProtectionRef(source)
	if again == nil || *again != ref {
		t.Fatal("caller altered original occurrence")
	}
	if got, err := authoredRecipeProtectionRef(makeSource(nil)); err != nil || got != nil {
		t.Fatal("absent protection invented")
	}
	if _, err := authoredRecipeProtectionRef(makeSource(v2input.Ref{})); err == nil {
		t.Fatal("zero source accepted")
	}
	source.RecipeJCS += " "
	if _, err := authoredRecipeProtectionRef(source); err == nil {
		t.Fatal("noncanonical recipe accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (SQLAuthoredDetailMaterialization{}).publishRecipeProtection(ctx, auth.Identity{}, source, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel crossed physical boundary")
	}
}
func TestAuthoredGlobalProtectionRetainsMetadataThroughActualMaskPath(t *testing.T) {
	v, docs := detailMaterializationFixture(t)
	var original struct {
		Protected []json.RawMessage `json:"protected"`
	}
	if json.Unmarshal([]byte(v.Original), &original) != nil || len(original.Protected) != 1 {
		t.Fatal("actual vector changed")
	}
	out, used, err := compileAuthoredMaskPaths(original.Protected, []byte(v.Current), v.CurrentRef, docs)
	if err != nil || len(out) != 1 || len(used) != 1 || used[0] != 1 {
		t.Fatalf("global protection path: %v %v", used, err)
	}
	before, err := review.ParseMaskRef(original.Protected[0])
	if err != nil {
		t.Fatal(err)
	}
	after, err := review.ParseMaskRef(out[0])
	if err != nil {
		t.Fatal(err)
	}
	before.Artifact = after.Artifact
	before.Canvas = after.Canvas
	before.TransformChain = after.TransformChain
	a, _ := canonical.Marshal(before)
	b, _ := canonical.Marshal(after)
	if !after.Protected || !bytes.Equal(a, b) {
		t.Fatal("protection selection/metadata changed")
	}
	if _, _, err := compileAuthoredMaskPaths(original.Protected, []byte(v.Current), v.CurrentRef, docs[:1]); err == nil {
		t.Fatal("portrait region transform substituted for protection")
	}
}
