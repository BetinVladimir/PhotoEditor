package v2learning

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestTrainingDraftPinsOptionalReviewedEvidenceToRunAndEvent(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	tenant, actor, session := uuid.New(), uuid.New(), uuid.New()
	key := "reviewed-evidence"
	runID, err := v2input.RunID(tenant, actor, key)
	if err != nil {
		t.Fatal(err)
	}
	eventID := manifest.Examples[0].EventID
	ref, err := ReviewedDefectHoldoutRef(runID, eventID,
		strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Examples[0].RenderCase.DefectEvidence = &ref
	if _, err := PrepareTrainingDraft(tenant, actor, session, project,
		profile, key, 4096, request, manifest); err != nil {
		t.Fatal("exact reviewed evidence ref rejected", err)
	}
	// Admission proves deterministic ownership and digest syntax; physical evidence
	// readers separately verify the supplied digest against immutable bytes.
	for _, changed := range []v2input.Ref{
		{ArtifactID: uuid.New(), SHA256: ref.SHA256},
		{ArtifactID: ref.ArtifactID, SHA256: "invalid-sha"},
	} {
		manifest.Examples[0].RenderCase.DefectEvidence = &changed
		if _, err := PrepareTrainingDraft(tenant, actor, session, project,
			profile, key, 4096, request, manifest); err == nil {
			t.Fatal("foreign reviewed evidence ref accepted")
		}
	}
}

func TestTrainingDraftPinsOptionalSeriesEvidenceToRunAndEvent(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	tenant, actor, session := uuid.New(), uuid.New(), uuid.New()
	key := "series-evidence"
	runID, err := v2input.RunID(tenant, actor, key)
	if err != nil {
		t.Fatal(err)
	}
	eventID := manifest.Examples[0].EventID
	ref, err := SeriesHoldoutRef(runID, eventID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Examples[0].RenderCase.SeriesEvidence = &ref
	if _, err := PrepareTrainingDraft(tenant, actor, session, project,
		profile, key, 4096, request, manifest); err != nil {
		t.Fatal("exact series evidence ref rejected", err)
	}
	// Admission proves deterministic ownership and digest syntax; physical evidence
	// readers separately verify the supplied digest against immutable bytes.
	for _, changed := range []v2input.Ref{
		{ArtifactID: uuid.New(), SHA256: ref.SHA256},
		{ArtifactID: ref.ArtifactID, SHA256: "invalid-sha"},
	} {
		manifest.Examples[0].RenderCase.SeriesEvidence = &changed
		if _, err := PrepareTrainingDraft(tenant, actor, session, project,
			profile, key, 4096, request, manifest); err == nil {
			t.Fatal("foreign series evidence ref accepted")
		}
	}
}

func TestTrainingDraftRejectsForgedFrozenManifest(t *testing.T) {
	project, profile, request, manifest := compiledTrainingDraftFixture(t)
	prepare := func(value TrainingManifest) error {
		_, err := PrepareTrainingDraft(uuid.New(), uuid.New(), uuid.New(),
			project, profile, "test-key", 4096, request, value)
		return err
	}
	if err := prepare(manifest); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*TrainingManifest)
	}{
		{"wrong split", func(m *TrainingManifest) {
			m.Examples[0].Split = "train"
		}},
		{"duplicate event", func(m *TrainingManifest) {
			m.Examples[1].EventID = m.Examples[0].EventID
		}},
		{"duplicate lineage", func(m *TrainingManifest) {
			m.Examples[1].RenderCase.SourceLineageSHA =
				m.Examples[0].RenderCase.SourceLineageSHA
		}},
		{"missing protected", func(m *TrainingManifest) {
			m.Examples[0].RenderCase.Protected.ArtifactID = uuid.Nil
		}},
		{"invalid target", func(m *TrainingManifest) {
			m.Examples[0].Target.EV = 2
		}},
		{"unbound source", func(m *TrainingManifest) {
			m.Examples[0].RenderCase.Binding.Source.ArtifactID = uuid.New()
		}},
		{"unregistered defect evidence", func(m *TrainingManifest) {
			ref := m.Examples[0].RenderCase.WorkingImage
			ref.ArtifactID = uuid.New()
			m.Examples[0].RenderCase.DefectEvidence = &ref
		}},
		{"wrong subject", func(m *TrainingManifest) {
			m.Examples[0].SubjectGroupID = uuid.New()
		}},
	}
	for _, check := range tests {
		t.Run(check.name, func(t *testing.T) {
			changed := manifest
			changed.Examples = append([]ManifestExample(nil), manifest.Examples...)
			check.edit(&changed)
			if prepare(changed) == nil {
				t.Fatal("forged manifest accepted")
			}
		})
	}
}
