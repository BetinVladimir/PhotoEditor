package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestReviewedDefectRecipeMaterialBindsBothBodiesAndRefs(t *testing.T) {
	base, approved := []byte(`{"base":1}`), []byte(`{"approved":1}`)
	sha := func(body []byte) string {
		sum := sha256.Sum256(body)
		return hex.EncodeToString(sum[:])
	}
	recipes := CompiledTrainingRecipes{BaseBody: base, ApprovedBody: approved,
		BaseSHA: sha(base), ApprovedSHA: sha(approved)}
	runID, eventID := uuid.New(), uuid.New()
	baseRef, approvedRef, err := recipes.Refs(runID, eventID)
	if err != nil {
		t.Fatal(err)
	}
	render := TrainingRenderCase{
		BaseRecipe: baseRef, ApprovedRecipe: approvedRef,
	}
	if !validReviewedDefectRecipeMaterial(runID, eventID, render, recipes) {
		t.Fatal("exact frozen recipe material rejected")
	}
	if validReviewedDefectRecipeMaterial(uuid.New(), eventID, render, recipes) ||
		validReviewedDefectRecipeMaterial(runID, uuid.New(), render, recipes) {
		t.Fatal("foreign run or event accepted")
	}
	for _, mutate := range []func(*TrainingRenderCase, *CompiledTrainingRecipes){
		func(_ *TrainingRenderCase, r *CompiledTrainingRecipes) { r.BaseBody = []byte(`{"base":2}`) },
		func(_ *TrainingRenderCase, r *CompiledTrainingRecipes) { r.ApprovedBody = []byte(`{"approved":2}`) },
		func(_ *TrainingRenderCase, r *CompiledTrainingRecipes) { r.BaseSHA = strings.Repeat("0", 64) },
		func(c *TrainingRenderCase, _ *CompiledTrainingRecipes) {
			c.ApprovedRecipe.SHA256 = strings.Repeat("0", 64)
		},
		func(c *TrainingRenderCase, _ *CompiledTrainingRecipes) {
			c.ApprovedRecipe.ArtifactID = c.BaseRecipe.ArtifactID
		},
	} {
		changedRender, changedRecipes := render, recipes
		mutate(&changedRender, &changedRecipes)
		if validReviewedDefectRecipeMaterial(runID, eventID, changedRender, changedRecipes) {
			t.Fatal("substituted frozen recipe material accepted")
		}
	}
}
