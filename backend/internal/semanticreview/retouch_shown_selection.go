package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// validateShownRetouchSelection binds explicit photographer approval to the
// physically checked producer and the current server-owned revision fence.
// It performs no IO and grants no permission to schedule or commit Apply.
func validateShownRetouchSelection(decision TargetDecision, fence PreviewRequest,
	selected LockedRetouchPreviewSelection, physical PhysicalRetouchPreview) error {
	if decision.Decision != "approve" || decision.Preview == nil || decision.TargetID == uuid.Nil ||
		selected.TargetID != decision.TargetID || selected.PreviewRunID == uuid.Nil || !physical.verified || fence.Validate() != nil {
		return ErrInvalid
	}
	p := physical.linkage.Presentation.Params
	if p.TargetID != decision.TargetID || p.WorkflowRevision != fence.ExpectedRevision || p.TargetRevision != fence.TargetRevision ||
		p.Plan.SHA256 != fence.PlanSHA || p.Candidate.SHA256 != fence.CandidateSHA ||
		physical.linkage.Document.TaskID != uuid.NewSHA1(selected.PreviewRunID, []byte("semantic.preview_retouch_region:"+decision.TargetID.String())) {
		return ErrConflict
	}
	_, digest, err := FreezeRetouchPreviewParams(p)
	if err != nil { return ErrInvalid }
	raw, err := canonical.Marshal(selected.Receipt)
	if err != nil { return ErrInvalid }
	if _, err = decodeRetouchPreviewReceipt(raw, physical.linkage, digest); err != nil { return ErrConflict }
	shown := decision.Preview
	if shown.Render.ArtifactID != selected.Receipt.WorkingImage.ArtifactID || shown.Render.SHA256 != selected.Receipt.WorkingImage.SHA256 ||
		shown.Evidence.ArtifactID != selected.Receipt.Presentation.ArtifactID || shown.Evidence.SHA256 != selected.Receipt.Presentation.SHA256 {
		return ErrConflict
	}
	return nil
}
