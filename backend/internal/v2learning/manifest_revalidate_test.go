package v2learning

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func fixtureManifestCandidates() (TrainingManifest, []TrainingCandidate) {
	manifest := TrainingManifest{SchemaVersion: 1, ProfileID: uuid.New()}
	consent := uuid.New()
	manifest.Consents = []TrainingConsent{{ConsentID: consent, Epoch: "3"}}
	sessions := make([]uuid.UUID, 6)
	for i := range sessions {
		sessions[i] = uuid.New()
	}
	rows := make([]TrainingCandidate, 40)
	for i := range rows {
		event, asset, approval := uuid.New(), uuid.New(), uuid.New()
		base, approved := uuid.New(), uuid.New()
		lineage := strings.Repeat("a", 64)
		rows[i] = TrainingCandidate{EventID: event, SessionID: sessions[i%6],
			AssetVersionID: asset, SourceLineageSHA: lineage,
			BaseArtifactID: base, BaseSHA: strings.Repeat("b", 64),
			ApprovedArtifactID: approved, ApprovedSHA: strings.Repeat("c", 64),
			ApprovalID: &approval, ConsentID: consent, ConsentEpoch: 3,
			ConsentLive: true, SourceReady: true, BaseReady: true,
			ApprovedReady: true, ChangedPaths: []string{"/ev"}}
		manifest.Examples = append(manifest.Examples, ManifestExample{
			EventID: event, SessionID: sessions[i%6],
			RenderCase: TrainingRenderCase{
				Binding:              v2input.SessionVersionBinding{AssetVersionID: asset},
				BaseRecipe:           v2input.Ref{ArtifactID: base, SHA256: rows[i].BaseSHA},
				ApprovedRecipe:       v2input.Ref{ArtifactID: approved, SHA256: rows[i].ApprovedSHA},
				BaseDocument:         v2input.Ref{ArtifactID: base, SHA256: rows[i].BaseSHA},
				ApprovedDocument:     v2input.Ref{ArtifactID: approved, SHA256: rows[i].ApprovedSHA},
				DescriptorShard:      v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)},
				DescriptorAnalysisID: uuid.New(), DescriptorOrdinal: 0,
				ApprovalID: approval, SourceLineageSHA: lineage}})
	}
	return manifest, rows
}

func TestManifestRevalidationFencesRelationalChanges(t *testing.T) {
	manifest, rows := fixtureManifestCandidates()
	if err := MatchTrainingManifestCandidates(manifest, rows); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		edit func([]TrainingCandidate)
	}{
		{"revoked consent", func(items []TrainingCandidate) { items[0].ConsentLive = false }},
		{"epoch advanced", func(items []TrainingCandidate) { items[0].ConsentEpoch++ }},
		{"approval changed", func(items []TrainingCandidate) { id := uuid.New(); items[0].ApprovalID = &id }},
		{"recipe replaced", func(items []TrainingCandidate) { items[0].ApprovedArtifactID = uuid.New() }},
		{"lineage changed", func(items []TrainingCandidate) { items[0].SourceLineageSHA = strings.Repeat("d", 64) }},
		{"source unavailable", func(items []TrainingCandidate) { items[0].SourceReady = false }},
		{"nonparametric edit", func(items []TrainingCandidate) { items[0].ChangedPaths = []string{"/crop"} }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			changed := append([]TrainingCandidate(nil), rows...)
			check.edit(changed)
			if err := MatchTrainingManifestCandidates(manifest, changed); err == nil {
				t.Fatal("changed source accepted")
			}
		})
	}
}
