package v2learning

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

func TestFitCompilerIncludesEveryFrozenExample(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	draft, err := PrepareTrainingDraft(uuid.New(), uuid.New(), uuid.New(),
		project, profile, "fit-test", 4096, request, manifest)
	if err != nil {
		t.Fatal(err)
	}
	body, err := CompileFitParams(draft, request.Policy)
	if err != nil {
		t.Fatal(err)
	}
	var params LearningFitParams
	if err := json.Unmarshal(body, &params); err != nil {
		t.Fatal(err)
	}
	manifestID, _ := TrainingDocumentID(draft.RunID, trainingManifestPurpose)
	if params.Manifest != (v2input.Ref{ArtifactID: manifestID, SHA256: draft.ManifestSHA}) ||
		params.BaseProfile != manifest.Baseline {
		t.Fatal("fit params changed frozen refs")
	}
	fitRef := artifactjson.Ref{ID: uuid.New(), SHA256: draft.ManifestSHA}
	closure, err := CompileFitClosure(draft, request.Policy, fitRef)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[uuid.UUID]string)
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
	for _, example := range manifest.Examples {
		if seen[example.RenderCase.WorkingImage.ArtifactID] != "WorkingImage" ||
			seen[example.RenderCase.BaseRecipe.ArtifactID] != "FrozenRecipe" ||
			seen[example.RenderCase.ApprovedRecipe.ArtifactID] != "FrozenRecipe" ||
			seen[example.RenderCase.Protected.ArtifactID] != "MaskRef" ||
			seen[example.RenderCase.Faces.ArtifactID] != "FaceObservations" {
			t.Fatal("fit closure omitted a frozen render case")
		}
	}
	request.Policy.Seed++
	if _, err := CompileFitParams(draft, request.Policy); err == nil {
		t.Fatal("changed policy accepted after freeze")
	}
	request.Policy.Seed--
	draft.Manifest.Examples[0].EventID = uuid.New()
	if _, err := CompileFitParams(draft, request.Policy); err == nil {
		t.Fatal("mutated frozen manifest accepted")
	}
}

func TestFitClosureDeclaresPairedIdentityEvidence(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	effective := v2input.Ref{ArtifactID: uuid.New(), SHA256: manifest.Baseline.SHA256}
	consent := v2input.Ref{ArtifactID: uuid.New(), SHA256: manifest.Calibration.SHA256}
	manifest.Examples[0].RenderCase.EffectiveMask = &effective
	manifest.Examples[0].RenderCase.ConsentSnapshot = &consent
	region := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("7", 64)}
	manifest.Examples[0].RenderCase.RegionMasks = []TrainingRegionMask{
		{RegionID: "sky", Mask: region}}
	draft, err := PrepareTrainingDraft(uuid.New(), uuid.New(), uuid.New(),
		project, profile, "identity-closure", 4096, request, manifest)
	if err != nil {
		t.Fatal(err)
	}
	params := artifactjson.Ref{ID: uuid.New(), SHA256: draft.ManifestSHA}
	closure, err := CompileFitClosure(draft, request.Policy, params)
	if err != nil {
		t.Fatal(err)
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
	if seen[effective.ArtifactID] != "MaskRef" ||
		seen[consent.ArtifactID] != "ConsentSnapshot" ||
		seen[region.ArtifactID] != "MaskRef" {
		t.Fatal("fit closure omitted paired identity evidence")
	}
}
