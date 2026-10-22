package semanticreview

import (
	"testing"
	"strings"
	"github.com/google/uuid"
)

func TestShownRetouchSelectionBindsPhotographerChoiceToExactPreview(t *testing.T) {
	for _, mode := range []string{"valid", "reject", "implicit", "missing_preview", "target", "selected_target", "run", "unverified", "workflow_revision", "target_revision", "plan", "candidate", "invalid_fence", "render_id", "render_sha", "presentation_id", "presentation_sha", "swapped", "receipt_task", "receipt_result", "receipt_attempt", "receipt_fence", "receipt_params", "receipt_pixels", "receipt_evidence", "physical_target"} {
		t.Run(mode, func(t *testing.T) {
			_, _, selected, physical := shownRetouchCompileFixture(t)
			p := physical.linkage.Presentation.Params
			fence := PreviewRequest{ExpectedRevision: p.WorkflowRevision, TargetRevision: p.TargetRevision, PlanSHA: p.Plan.SHA256, CandidateSHA: p.Candidate.SHA256}
			decision := TargetDecision{TargetID: p.TargetID, Decision: "approve", Preview: &PreviewSelection{
				Render: TargetDocument{ArtifactID: selected.Receipt.WorkingImage.ArtifactID, SHA256: selected.Receipt.WorkingImage.SHA256},
				Evidence: TargetDocument{ArtifactID: selected.Receipt.Presentation.ArtifactID, SHA256: selected.Receipt.Presentation.SHA256}}}
			switch mode {
			case "reject": decision.Decision = "reject"
			case "implicit": decision.Decision = ""
			case "missing_preview": decision.Preview = nil
			case "target": decision.TargetID = uuid.New()
			case "selected_target": selected.TargetID = uuid.New()
			case "run": selected.PreviewRunID = uuid.New()
			case "unverified": physical.verified = false
			case "workflow_revision": fence.ExpectedRevision = "999"
			case "target_revision": fence.TargetRevision = "999"
			case "plan": fence.PlanSHA = strings.Repeat("b",64)
			case "candidate": fence.CandidateSHA = strings.Repeat("b",64)
			case "invalid_fence": fence.TargetRevision = "01"
			case "render_id": decision.Preview.Render.ArtifactID = uuid.New()
			case "render_sha": decision.Preview.Render.SHA256 = strings.Repeat("b",64)
			case "presentation_id": decision.Preview.Evidence.ArtifactID = uuid.New()
			case "presentation_sha": decision.Preview.Evidence.SHA256 = strings.Repeat("b",64)
			case "swapped": decision.Preview.Render, decision.Preview.Evidence = decision.Preview.Evidence, decision.Preview.Render
			case "receipt_task": selected.Receipt.TaskID = uuid.New()
			case "receipt_result": selected.Receipt.ResultID = uuid.New()
			case "receipt_attempt": selected.Receipt.Attempt++
			case "receipt_fence": selected.Receipt.Fence++
			case "receipt_params": selected.Receipt.ParamsSHA = strings.Repeat("b",64)
			case "receipt_pixels": selected.Receipt.Pixels.ArtifactID = uuid.New()
			case "receipt_evidence": selected.Receipt.NumericalEvidence.ArtifactID = uuid.New()
			case "physical_target": physical.linkage.Presentation.Params.TargetID = uuid.New()
			}
			err := validateShownRetouchSelection(decision, fence, selected, physical)
			if (err == nil) != (mode == "valid") { t.Fatal("shown selection mismatch", mode, err) }
		})
	}
}
