package v2learning

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestSeriesHoldoutRecoveryRetainsExactProducerBytes(t *testing.T) {
	document := seriesHoldoutFixture()
	body, sha, err := CompileFrozenSeriesHoldoutEvidence(document)
	if err != nil {
		t.Fatal(err)
	}
	trainingRun := uuid.New()
	ref, err := SeriesHoldoutRef(trainingRun, document.EventID, sha)
	if err != nil {
		t.Fatal(err)
	}
	draft := TrainingDraftPrepared{TenantID: document.TenantID, RunID: trainingRun}
	draft.Manifest.Examples = []ManifestExample{{EventID: document.EventID,
		RenderCase: TrainingRenderCase{SeriesEvidence: &ref,
			Binding: v2input.SessionVersionBinding{
				AssetVersionID: document.AssetVersionID,
				Source:         document.SourceImage},
			WorkingImage: document.SourceImage}}}
	draft.RecoveryBody, err = canonical.Marshal(trainingDraftRecovery{SchemaVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	prepared := PreparedSeriesHoldout{Document: document, Body: body, SHA256: sha}
	draft, err = AttachSeriesHoldoutRecovery(draft,
		map[uuid.UUID]PreparedSeriesHoldout{document.EventID: prepared})
	if err != nil {
		t.Fatal(err)
	}
	var saved trainingDraftRecovery
	if json.Unmarshal(draft.RecoveryBody, &saved) != nil || saved.SeriesJCS == "" {
		t.Fatal("missing series recovery bytes")
	}
	restored, err := seriesHoldoutFromRecovery(draft, []byte(saved.SeriesJCS))
	if err != nil || restored[document.EventID].SHA256 != sha ||
		string(restored[document.EventID].Body) != string(body) {
		t.Fatal("series recovery changed publication bytes", err)
	}
	if _, err := seriesHoldoutFromRecovery(draft, nil); err == nil {
		t.Fatal("missing series recovery accepted")
	}
	foreign := draft
	foreign.Manifest.Examples = append([]ManifestExample(nil), draft.Manifest.Examples...)
	foreign.Manifest.Examples[0].RenderCase.SeriesEvidence = &v2input.Ref{
		ArtifactID: uuid.New(), SHA256: sha}
	if _, err := seriesHoldoutFromRecovery(foreign,
		[]byte(saved.SeriesJCS)); err == nil {
		t.Fatal("foreign manifest artifact accepted")
	}
	if _, err := seriesHoldoutFromRecovery(draft,
		append([]byte(saved.SeriesJCS), ' ')); err == nil {
		t.Fatal("noncanonical recovery accepted")
	}
}
