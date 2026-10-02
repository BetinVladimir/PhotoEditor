package v2learning

import (
	"testing"

	"github.com/google/uuid"
)

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
