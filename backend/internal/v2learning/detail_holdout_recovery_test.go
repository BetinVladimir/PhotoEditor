package v2learning

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestPortraitDetailRecoveryRetainsExactProducerBytes(t *testing.T) {
	origin := detailEvidenceOrigin()
	origin.MaskArtifacts = []v2input.Ref{detailEvidenceRef(12), detailEvidenceRef(13)}
	document, body, sha, err := CompileFrozenPortraitDetailHoldoutEvidence(
		origin, origin.MaskArtifacts)
	if err != nil {
		t.Fatal(err)
	}
	trainingRun := uuid.New()
	ref, err := PortraitDetailHoldoutRef(trainingRun, origin.EventID, sha)
	if err != nil {
		t.Fatal(err)
	}
	draft := TrainingDraftPrepared{TenantID: origin.TenantID, RunID: trainingRun}
	draft.Manifest.Examples = []ManifestExample{{EventID: origin.EventID,
		RenderCase: TrainingRenderCase{DetailEvidence: &ref,
			Binding: v2input.SessionVersionBinding{
				AssetVersionID: origin.AssetVersionID, Source: origin.Source},
			WorkingImage: origin.Source, Protected: origin.Protection}}}
	draft.RecoveryBody, err = canonical.Marshal(trainingDraftRecovery{SchemaVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	prepared := PreparedPortraitDetailHoldout{Document: document, Body: body,
		SHA256: sha}
	draft, err = AttachPortraitDetailRecovery(draft,
		map[uuid.UUID]PreparedPortraitDetailHoldout{origin.EventID: prepared})
	if err != nil {
		t.Fatal(err)
	}
	var saved trainingDraftRecovery
	if json.Unmarshal(draft.RecoveryBody, &saved) != nil || saved.DetailJCS == "" {
		t.Fatal("missing detail recovery bytes")
	}
	restored, err := portraitDetailFromRecovery(draft, []byte(saved.DetailJCS))
	if err != nil || restored[origin.EventID].SHA256 != sha ||
		string(restored[origin.EventID].Body) != string(body) {
		t.Fatal("detail recovery changed publication bytes", err)
	}
	if _, err := portraitDetailFromRecovery(draft, nil); err == nil {
		t.Fatal("missing detail recovery accepted")
	}
	foreign := draft
	foreign.Manifest.Examples = append([]ManifestExample(nil), draft.Manifest.Examples...)
	foreign.Manifest.Examples[0].RenderCase.DetailEvidence = &v2input.Ref{
		ArtifactID: uuid.New(), SHA256: sha}
	if _, err := portraitDetailFromRecovery(foreign,
		[]byte(saved.DetailJCS)); err == nil {
		t.Fatal("foreign manifest artifact accepted")
	}
	changed := saved
	changed.DetailJCS = saved.DetailJCS + " "
	if _, err := portraitDetailFromRecovery(draft,
		[]byte(changed.DetailJCS)); err == nil {
		t.Fatal("noncanonical recovery accepted")
	}
}
