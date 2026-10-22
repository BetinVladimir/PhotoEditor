package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestDecisionPreviewSelectionRejectsAliasAndRejectProof(t *testing.T) {
	hash := previewParamsFixture().Plan.SHA256
	for _, kind := range []string{"valid", "alias", "reject", "sha"} {
		selection := PreviewSelection{Render: TargetDocument{ArtifactID: uuid.New(), SHA256: hash}, Evidence: TargetDocument{ArtifactID: uuid.New(), SHA256: hash}}
		decision := "approve"
		if kind == "alias" {
			selection.Evidence = selection.Render
		}
		if kind == "reject" {
			decision = "reject"
		}
		if kind == "sha" {
			selection.Evidence.SHA256 = ""
		}
		body, err := canonical.Marshal(ApplyDecision{ExpectedRevision: "1", PlanSHA: hash, TargetDecisions: []TargetDecision{{TargetID: uuid.New(), Decision: decision, Preview: &selection}}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = DecodeApplyDecision(body)
		if (err == nil) != (kind == "valid") {
			t.Fatal(kind, err)
		}
	}
}
