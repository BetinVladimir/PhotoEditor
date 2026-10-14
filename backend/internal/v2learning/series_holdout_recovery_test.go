package v2learning

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func seriesStatsRefsFixture() []v2input.Ref {
	return []v2input.Ref{
		{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)},
		{ArtifactID: uuid.New(), SHA256: strings.Repeat("f", 64)},
	}
}

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
	prepared := PreparedSeriesHoldout{Document: document, Body: body,
		SHA256: sha, Stats: seriesStatsRefsFixture()}
	draft, err = AttachSeriesHoldoutRecovery(draft,
		map[uuid.UUID]PreparedSeriesHoldout{document.EventID: prepared})
	if err != nil {
		t.Fatal(err)
	}
	var saved trainingDraftRecovery
	if json.Unmarshal(draft.RecoveryBody, &saved) != nil ||
		saved.SeriesJCS == "" || saved.SeriesStatsJCS == "" {
		t.Fatal("missing series recovery bytes")
	}
	restored, err := seriesHoldoutFromRecovery(draft, []byte(saved.SeriesJCS))
	if err != nil || restored[document.EventID].SHA256 != sha ||
		string(restored[document.EventID].Body) != string(body) {
		t.Fatal("series recovery changed publication bytes", err)
	}
	stats, err := seriesStatsFromRecovery(draft, []byte(saved.SeriesStatsJCS))
	if err != nil || len(stats[document.EventID]) != 2 ||
		stats[document.EventID][0] != prepared.Stats[0] {
		t.Fatal("series stats recovery lost exact refs", err)
	}
	if _, err := seriesStatsFromRecovery(draft, nil); err == nil {
		t.Fatal("missing series stats recovery accepted")
	}
	var statItems []seriesStatsRecoveryItem
	if json.Unmarshal([]byte(saved.SeriesStatsJCS), &statItems) != nil {
		t.Fatal("invalid stats fixture")
	}
	statItems[0].Stats[1] = statItems[0].Stats[0]
	duplicate, err := canonical.Marshal(statItems)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seriesStatsFromRecovery(draft, duplicate); err == nil {
		t.Fatal("duplicate stats ref accepted")
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
