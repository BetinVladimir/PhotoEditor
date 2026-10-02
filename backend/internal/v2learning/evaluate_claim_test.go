package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestEvaluateParamsPinCanonicalCandidateAndFrozenLocks(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	draft, err := PrepareTrainingDraft(uuid.New(), uuid.New(), uuid.New(),
		project, profile, "evaluate-test", 4096, request, manifest)
	if err != nil {
		t.Fatal(err)
	}
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Minute), TenantID: draft.TenantID,
		RunID: draft.RunID, WorkflowID: draft.WorkflowID,
		Candidate: v2input.Ref{ArtifactID: uuid.New(),
			SHA256: strings.Repeat("a", 64)}}
	body, err := CompileEvaluateParams(lease, draft)
	if err != nil {
		t.Fatal(err)
	}
	var params LearningEvaluateParams
	if err := json.Unmarshal(body, &params); err != nil {
		t.Fatal(err)
	}
	manifestID, _ := TrainingDocumentID(draft.RunID, trainingManifestPurpose)
	if params.Candidate != lease.Candidate ||
		params.Manifest != (v2input.Ref{ArtifactID: manifestID,
			SHA256: draft.ManifestSHA}) ||
		params.Baseline != manifest.Baseline ||
		params.Calibration != manifest.Calibration ||
		params.ModelLockSHA != manifest.ModelLockSHA ||
		params.ICCLockSHA != manifest.ICCLockSHA {
		t.Fatal("evaluation parameters did not bind frozen inputs")
	}
	lease.RunID = uuid.New()
	if _, err := CompileEvaluateParams(lease, draft); err == nil {
		t.Fatal("foreign run lease accepted")
	}
	lease.RunID = draft.RunID
	draft.Manifest.Examples[0].EventID = uuid.New()
	if _, err := CompileEvaluateParams(lease, draft); err == nil {
		t.Fatal("mutated frozen manifest accepted")
	}
}

func TestEvaluateClosureIncludesHoldoutAndRejectsConflictingRefs(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	resolved := make([]LearningRenderInputs, 0)
	for index := range manifest.Examples {
		example := &manifest.Examples[index]
		if example.Split != "holdout" {
			continue
		}
		style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}
		recipe := FrozenRecipe{SchemaVersion: 1, Source: example.RenderCase.Binding,
			BaseStyle: style, Renderer: manifest.RendererVersion,
			ICCLockSHA: manifest.ICCLockSHA, ModelLockSHA: manifest.ModelLockSHA}
		body, err := canonical.Marshal(recipe)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		sha := hex.EncodeToString(sum[:])
		example.RenderCase.BaseRecipe.SHA256 = sha
		example.RenderCase.ApprovedRecipe.SHA256 = sha
		resolved = append(resolved, LearningRenderInputs{
			EventID: example.EventID, WorkingImage: example.RenderCase.WorkingImage,
			PixelCount:      512 * 512,
			Pixels:          v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)},
			Protected:       example.RenderCase.Protected,
			ProtectedPixels: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)},
			BaseStyle:       style, BaseRecipeBody: body, ApprovedRecipeBody: body,
		})
	}
	regionMask := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("1", 64)}
	regionPixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("2", 64)}
	for index := range manifest.Examples {
		if manifest.Examples[index].Split == "holdout" {
			manifest.Examples[index].RenderCase.RegionMasks = []TrainingRegionMask{
				{RegionID: "sky", Mask: regionMask}}
			break
		}
	}
	resolved[0].RegionMasks = []LearningRegionPixels{{
		RegionID: "sky", Mask: regionMask, Pixels: regionPixels}}
	draft, err := PrepareTrainingDraft(uuid.New(), uuid.New(), uuid.New(),
		project, profile, "evaluate-closure", 4096, request, manifest)
	if err != nil {
		t.Fatal(err)
	}
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Minute), TenantID: draft.TenantID,
		RunID: draft.RunID, WorkflowID: draft.WorkflowID,
		Candidate: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}}
	ref := artifactjson.Ref{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	closure, err := CompileEvaluateClosure(lease, draft, ref, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(closure.InputDigest) != 64 || len(closure.Direct) != 5 {
		t.Fatal("incomplete evaluate closure")
	}
	var admitted evaluateProfile
	if err := json.Unmarshal(closure.Profile, &admitted); err != nil {
		t.Fatal(err)
	}
	if admitted.ResourceEstimate.PixelCount != int64(len(resolved))*512*512 ||
		admitted.ResourceEstimate.ScratchBytes != 100*512*512+536870912 ||
		admitted.ResourceEstimate.RAMBytes != 4294967296 {
		t.Fatal("resource budget did not follow frozen pixels")
	}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uuid.UUID]string{}
	for {
		entry, okay, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !okay {
			break
		}
		seen[entry.Ref.ArtifactID] = entry.Contract
	}
	if seen[lease.Candidate.ArtifactID] != "CandidateProfile" ||
		seen[ref.ID] != "Params" ||
		seen[regionMask.ArtifactID] != "MaskRef" ||
		seen[regionPixels.ArtifactID] != "MaskArray" {
		t.Fatal("canonical direct inputs absent")
	}
	for _, example := range manifest.Examples {
		if example.Split == "holdout" {
			if seen[example.RenderCase.WorkingImage.ArtifactID] != "WorkingImage" ||
				seen[example.RenderCase.BaseRecipe.ArtifactID] != "FrozenRecipe" ||
				seen[example.RenderCase.ApprovedRecipe.ArtifactID] != "FrozenRecipe" {
				t.Fatal("holdout render closure absent")
			}
		}
	}
	for _, item := range resolved {
		if seen[item.Pixels.ArtifactID] != "NpyArray" ||
			seen[item.ProtectedPixels.ArtifactID] != "MaskArray" ||
			seen[item.BaseStyle.ArtifactID] != "StyleV2" {
			t.Fatal("transitive render inputs absent")
		}
	}
	if _, err := CompileEvaluateClosure(lease, draft, ref, resolved[:len(resolved)-1]); err == nil {
		t.Fatal("missing holdout pixels accepted")
	}
	wrong := append([]LearningRenderInputs(nil), resolved...)
	wrong[0].Protected = v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	if _, err := CompileEvaluateClosure(lease, draft, ref, wrong); err == nil {
		t.Fatal("unbound protected mask accepted")
	}
	wrong = append([]LearningRenderInputs(nil), resolved...)
	wrong[0].PixelCount = 200_000_001
	if _, err := CompileEvaluateClosure(lease, draft, ref, wrong); err == nil {
		t.Fatal("oversized holdout image accepted")
	}
	wrong = append([]LearningRenderInputs(nil), resolved...)
	wrong[0].ApprovedRecipeBody = []byte(`{"schema_version":1}`)
	if _, err := CompileEvaluateClosure(lease, draft, ref, wrong); err == nil {
		t.Fatal("unverified approved recipe accepted")
	}
	wrong = append([]LearningRenderInputs(nil), resolved...)
	wrong[0].BaseStyle.ArtifactID = uuid.New()
	if _, err := CompileEvaluateClosure(lease, draft, ref, wrong); err == nil {
		t.Fatal("self-declared style accepted without recipe binding")
	}
	wrong = append([]LearningRenderInputs(nil), resolved...)
	wrong[0].RegionMasks = []LearningRegionPixels{{
		RegionID: "sky", Mask: regionMask,
		Pixels: v2input.Ref{ArtifactID: uuid.New(), SHA256: "bad"}}}
	if _, err := CompileEvaluateClosure(lease, draft, ref, wrong); err == nil {
		t.Fatal("invalid region child accepted")
	}
	for index := range draft.Manifest.Examples {
		if draft.Manifest.Examples[index].Split == "holdout" {
			effective := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}
			consent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)}
			draft.Manifest.Examples[index].RenderCase.EffectiveMask = &effective
			draft.Manifest.Examples[index].RenderCase.ConsentSnapshot = &consent
			if _, err := CompileEvaluateClosure(lease, draft, ref, resolved); err == nil {
				t.Fatal("identity refs accepted without verified resolver inputs")
			}
			draft.Manifest.Examples[index].RenderCase.EffectiveMask = nil
			draft.Manifest.Examples[index].RenderCase.ConsentSnapshot = nil
			break
		}
	}
	draft.Manifest.Examples[0].Split = "train"
	if _, err := CompileEvaluateClosure(lease, draft, ref, resolved); err == nil {
		t.Fatal("mutated manifest accepted")
	}
}
