package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestCompileTrainingDraftLinksInventorySplitAndFrozenCases(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	policySHA, err := canonical.Hash(request.Policy)
	if err != nil {
		t.Fatal(err)
	}
	context := ManifestContext{ProfileID: profile, ProjectID: project,
		Mode: manifest.Mode, PolicySHA: policySHA,
		Baseline: manifest.Baseline, ICCLockSHA: manifest.ICCLockSHA,
		ModelLockSHA: manifest.ModelLockSHA, Calibration: manifest.Calibration}
	sources := make(map[uuid.UUID]EligibleTrainingExample)
	for _, source := range trainingCorpus() {
		sources[source.EventID] = source
	}
	consent := manifest.Consents[0].ConsentID
	candidates := make([]TrainingCandidate, 0, len(manifest.Examples))
	evidence := make([]TrainingCaseEvidence, 0, len(manifest.Examples))
	for _, item := range manifest.Examples {
		style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
		recipe := FrozenRecipe{SchemaVersion: 1, Source: item.RenderCase.Binding,
			BaseStyle: style, Renderer: "v2.2", ICCLockSHA: manifest.ICCLockSHA,
			ModelLockSHA: manifest.ModelLockSHA}
		body, marshalErr := canonical.Marshal(recipe)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		sum := sha256.Sum256(body)
		sha := hex.EncodeToString(sum[:])
		frozen := &CompiledTrainingRecipes{Base: recipe, Approved: recipe,
			BaseBody: body, ApprovedBody: body, BaseSHA: sha, ApprovedSHA: sha}
		item.RenderCase.BaseRecipe.SHA256 = sha
		item.RenderCase.ApprovedRecipe.SHA256 = sha
		source := sources[item.EventID]
		approval := item.RenderCase.ApprovalID
		candidate := TrainingCandidate{EventID: item.EventID,
			SessionID:          item.SessionID,
			AssetVersionID:     item.RenderCase.Binding.AssetVersionID,
			SourceContentSHA:   source.SourceContentSHA,
			SourceLineageSHA:   source.SourceLineageSHA,
			BaseDocumentID:     uuid.New(),
			BaseArtifactID:     item.RenderCase.BaseRecipe.ArtifactID,
			BaseSHA:            item.RenderCase.BaseRecipe.SHA256,
			ApprovedDocumentID: uuid.New(),
			ApprovedArtifactID: item.RenderCase.ApprovedRecipe.ArtifactID,
			ApprovedSHA:        item.RenderCase.ApprovedRecipe.SHA256,
			ApprovalID:         &approval, ApprovedAt: source.ApprovedAt,
			ConsentID: consent, ConsentEpoch: 1,
			ChangedPaths: []string{"/ev"}, ConsentLive: true,
			SourceReady: true, BaseReady: true, ApprovedReady: true}
		candidates = append(candidates, candidate)
		evidence = append(evidence, TrainingCaseEvidence{Source: source,
			Resolved: ResolvedTrainingCase{Candidate: candidate,
				Render: item.RenderCase}, Frozen: frozen})
	}
	compile := func(rows []TrainingCandidate, cases []TrainingCaseEvidence) (
		TrainingCompilation, error) {
		return CompileTrainingDraft(uuid.New(), uuid.New(), uuid.New(),
			project, profile, "training-key", 4096, request, context,
			rows, cases)
	}
	result, err := compile(candidates, evidence)
	if err != nil || len(result.Draft.Manifest.Examples) != 60 ||
		result.Draft.Manifest.ProfileID != profile || len(result.Excluded) != 0 ||
		len(result.FrozenRecipes) != 60 {
		t.Fatalf("training compiler failed: %v", err)
	}
	seriesTenant, seriesActor, seriesSession := uuid.New(), uuid.New(), uuid.New()
	seriesCases := append([]TrainingCaseEvidence(nil), evidence...)
	seriesDocument := seriesHoldoutFixture()
	seriesDocument.TenantID = seriesTenant
	seriesDocument.EventID = seriesCases[0].Source.EventID
	seriesDocument.AssetVersionID = seriesCases[0].Resolved.Candidate.AssetVersionID
	seriesDocument.SourceImage = seriesCases[0].Resolved.Render.WorkingImage
	seriesBody, seriesSHA, err := CompileFrozenSeriesHoldoutEvidence(seriesDocument)
	if err != nil {
		t.Fatal(err)
	}
	seriesCases[0].Series = &PreparedSeriesHoldout{
		Document: seriesDocument, Body: seriesBody, SHA256: seriesSHA,
		Stats: seriesStatsRefsFixture()}
	seriesResult, err := CompileTrainingDraft(seriesTenant, seriesActor,
		seriesSession, project, profile, "series-key", 4096, request,
		context, candidates, seriesCases)
	if err != nil || len(seriesResult.Series) != 1 {
		t.Fatalf("series evidence dropped during compilation: %v", err)
	}
	var seriesExample *ManifestExample
	for i := range seriesResult.Draft.Manifest.Examples {
		if seriesResult.Draft.Manifest.Examples[i].EventID == seriesDocument.EventID {
			seriesExample = &seriesResult.Draft.Manifest.Examples[i]
		}
	}
	if seriesExample == nil || seriesExample.RenderCase.SeriesEvidence == nil ||
		seriesExample.RenderCase.SeriesEvidence.SHA256 != seriesSHA {
		t.Fatal("compiled manifest lost frozen series ref")
	}
	var seriesRecovery trainingDraftRecovery
	if json.Unmarshal(seriesResult.Draft.RecoveryBody, &seriesRecovery) != nil ||
		seriesRecovery.SeriesJCS == "" || seriesRecovery.SeriesStatsJCS == "" {
		t.Fatal("compiled recovery lost frozen series bytes")
	}
	missingFrozen := append([]TrainingCaseEvidence(nil), evidence...)
	missingFrozen[0].Frozen = nil
	if _, err := compile(candidates, missingFrozen); err == nil {
		t.Fatal("missing frozen recipe pair accepted")
	}
	bad := append([]TrainingCaseEvidence(nil), evidence...)
	bad[0].Source.SourceContentSHA = strings.Repeat("f", 64)
	if _, err := compile(candidates, bad); err == nil {
		t.Fatal("evidence disconnected from inventory")
	}
	bad = append([]TrainingCaseEvidence(nil), evidence[1:]...)
	if _, err := compile(candidates, bad); err == nil {
		t.Fatal("missing frozen case accepted")
	}
	artifactExcluded := []ExcludedTrainingCandidate{{EventID: candidates[0].EventID,
		Reason: "UNSUPPORTED_ARTIFACT"}}
	result, err = CompileTrainingDraftWithExclusions(uuid.New(), uuid.New(),
		uuid.New(), project, profile, "training-key", 4096, request, context,
		candidates, bad, artifactExcluded)
	if err != nil || len(result.Draft.Manifest.Examples) != 59 ||
		len(result.Excluded) != 1 ||
		result.Excluded[0] != artifactExcluded[0] {
		t.Fatalf("explicit artifact exclusion failed: %v", err)
	}
	artifactExcluded[0].EventID = uuid.New()
	if _, err := CompileTrainingDraftWithExclusions(uuid.New(), uuid.New(),
		uuid.New(), project, profile, "training-key", 4096, request, context,
		candidates, bad, artifactExcluded); err == nil {
		t.Fatal("accepted exclusion outside candidate inventory")
	}
	excluded := append([]TrainingCandidate(nil), candidates...)
	excluded[0].ConsentLive = false
	result, err = compile(excluded, evidence[1:])
	if err != nil || len(result.Draft.Manifest.Examples) != 59 ||
		len(result.Excluded) != 1 || result.Excluded[0].Reason != "CONSENT_REVOKED" {
		t.Fatalf("relational exclusion failed: %v", err)
	}
}
