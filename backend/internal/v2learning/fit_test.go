package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
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
	fitSum := sha256.Sum256(body)
	fitRef := artifactjson.Ref{ID: uuid.New(), SHA256: hex.EncodeToString(fitSum[:])}
	closure, err := CompileFitClosure(draft, request.Policy, fitRef)
	if err != nil {
		t.Fatal(err)
	}
	wrong := fitRef
	wrong.SHA256 = strings.Repeat("f", 64)
	if _, err := CompileFitClosure(draft, request.Policy, wrong); err == nil {
		t.Fatal("accepted substituted Fit parameter digest")
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

func TestFitClosureDeclaresReviewedDefectDocument(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	tenant, actor := uuid.New(), uuid.New()
	key := "defect-fit-closure"
	runID, err := v2input.RunID(tenant, actor, key)
	if err != nil {
		t.Fatal(err)
	}
	eventID := manifest.Examples[0].EventID
	ref, err := ReviewedDefectHoldoutRef(runID, eventID,
		strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Examples[0].RenderCase.DefectEvidence = &ref
	draft, err := PrepareTrainingDraft(tenant, actor, uuid.New(),
		project, profile, key, 4096, request, manifest)
	if err != nil {
		t.Fatal(err)
	}
	paramsBody, err := CompileFitParams(draft, request.Policy)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(paramsBody)
	params := artifactjson.Ref{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	closure, err := CompileFitClosure(draft, request.Policy, params)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		entry, okay, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !okay {
			break
		}
		if entry.Ref == ref {
			if entry.Contract != "FrozenDefectHoldoutEvidence" {
				t.Fatal("reviewed document declared with wrong contract")
			}
			count++
		}
	}
	if count != 1 {
		t.Fatal("reviewed document missing from fit closure")
	}
}

func TestFitClosureDeclaresExactSeriesRecoveryInputs(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	tenant, actor, key := uuid.New(), uuid.New(), "series-fit-closure"
	runID, err := v2input.RunID(tenant, actor, key)
	if err != nil {
		t.Fatal(err)
	}
	example := manifest.Examples[0]
	document := seriesHoldoutFixture()
	document.TenantID = tenant
	document.EventID = example.EventID
	document.AssetVersionID = example.RenderCase.Binding.AssetVersionID
	document.SourceImage = example.RenderCase.WorkingImage
	value, sha, err := CompileFrozenSeriesHoldoutEvidence(document)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := SeriesHoldoutRef(runID, example.EventID, sha)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Examples[0].RenderCase.SeriesEvidence = &ref
	stats := seriesStatsRefsFixture()
	draft, err := PrepareTrainingDraft(tenant, actor, uuid.New(), project,
		profile, key, 4096, request, manifest)
	if err != nil {
		t.Fatal(err)
	}
	draft, err = AttachSeriesHoldoutRecovery(draft,
		map[uuid.UUID]PreparedSeriesHoldout{example.EventID: {
			Document: document, Body: value, SHA256: sha, Stats: stats}})
	if err != nil {
		t.Fatal(err)
	}
	paramsBody, err := CompileFitParams(draft, request.Policy)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(paramsBody)
	params := artifactjson.Ref{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	closure, err := CompileFitClosure(draft, request.Policy, params)
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
	for _, want := range []v2input.Entry{
		{Ref: ref, Contract: "FrozenSeriesHoldoutEvidence"},
		{Ref: document.StageImage, Contract: "WorkingImage"},
		{Ref: document.Params, Contract: "SeriesParams"},
		{Ref: document.Series, Contract: "SeriesCorrections"},
		{Ref: stats[0], Contract: "SeriesStats"},
		{Ref: stats[1], Contract: "SeriesStats"},
	} {
		if seen[want.Ref.ArtifactID] != want.Contract {
			t.Fatalf("missing series input %s", want.Contract)
		}
	}
	missing := draft
	missing.RecoveryBody = nil
	if _, err := CompileFitClosure(missing, request.Policy, params); err == nil {
		t.Fatal("series evidence admitted without recovery bytes")
	}
	var recovery trainingDraftRecovery
	if err := json.Unmarshal(draft.RecoveryBody, &recovery); err != nil {
		t.Fatal(err)
	}
	recovery.SeriesStatsJCS = ""
	missing.RecoveryBody, err = canonical.Marshal(recovery)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileFitClosure(missing, request.Policy, params); err == nil {
		t.Fatal("series evidence admitted without frozen Stats refs")
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
	backgroundStyle := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("8", 64)}
	backgroundSource := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("9", 64)}
	manifest.Examples[0].RenderCase.BackgroundAsset = &v2draft.FrozenBackgroundAsset{SchemaVersion: 1, PortableAssetID: "installed", StyleVersionID: uuid.New(), Style: backgroundStyle, Source: backgroundSource, ExpectedBytes: 10, MediaType: "image/png", Suffix: ".png", WorkICCSHA: manifest.ICCLockSHA}
	draft, err := PrepareTrainingDraft(uuid.New(), uuid.New(), uuid.New(),
		project, profile, "identity-closure", 4096, request, manifest)
	if err != nil {
		t.Fatal(err)
	}
	body, err := CompileFitParams(draft, request.Policy)
	if err != nil {
		t.Fatal(err)
	}
	paramsSum := sha256.Sum256(body)
	params := artifactjson.Ref{ID: uuid.New(), SHA256: hex.EncodeToString(paramsSum[:])}
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
		seen[region.ArtifactID] != "MaskRef" ||
		seen[backgroundStyle.ArtifactID] != "StyleV2" || seen[backgroundSource.ArtifactID] != "StyleBackgroundAsset" {
		t.Fatal("fit closure omitted paired identity evidence")
	}
}

func TestLearningClosureCursorCancellationDoesNotConsumeEntry(t *testing.T) {
	entries := []v2input.Entry{
		{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Contract: "Params"},
		{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, Contract: "Calibration"},
	}
	for _, position := range []int{0, 1, 2} {
		cursor, err := openFitEntryCursor(context.Background(), entries)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < position; i++ {
			if _, okay, err := cursor.Next(context.Background()); !okay || err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if entry, okay, err := cursor.Next(ctx); okay || !errors.Is(err, context.Canceled) || entry != (v2input.Entry{}) {
			t.Fatalf("cancelled cursor returned entry: %v %v %v", entry, okay, err)
		}
		entry, okay, err := cursor.Next(context.Background())
		if err != nil || okay != (position < len(entries)) {
			t.Fatalf("cursor position changed: %v %v", okay, err)
		}
		if okay && entry != entries[position] {
			t.Fatal("cancelled read consumed an entry")
		}
		if opened, err := openFitEntryCursor(ctx, entries); opened != nil || !errors.Is(err, context.Canceled) {
			t.Fatal("opened cursor with cancelled context")
		}
	}
}
