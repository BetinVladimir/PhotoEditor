package semanticreview

import (
	"bytes"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strconv"
)

// VerifyShownBackgroundCleanupBatch binds every copy task to exactly one explicit human
// approval and the approved-edit document rebuilt from private review evidence.
// Rejections produce no task. It returns no partial batch on any mismatch.
// This is content verification; live locks/quota/activation remain mandatory.
func VerifyShownBackgroundCleanupBatch(tenant, intentID, child uuid.UUID, review PreparedReview, targets []LockedTarget,
	request ApplyDecision, tasks []PreparedShownBackgroundCleanup) ([]PreparedShownBackgroundCleanup, error) {
	if tenant == uuid.Nil || intentID == uuid.Nil || child == uuid.Nil || child == review.Sources.RunID {
		return nil, ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return nil, ErrInvalid
	}
	owned, err := DecodeApplyDecision(raw)
	if err != nil {
		return nil, err
	}
	drafts, err := CompileApprovedTaskDrafts(child, review, targets, owned)
	if err != nil {
		return nil, err
	}
	private, err := PrepareReviewDocuments(review.Sources, review.documents, review.modelSHA, review.datasetSHA)
	if err != nil {
		return nil, err
	}
	if len(drafts) != len(tasks) {
		return nil, ErrInvalid
	}
	decisions := map[uuid.UUID]TargetDecision{}
	for _, decision := range owned.TargetDecisions {
		decisions[decision.TargetID] = decision
	}
	targetRows := map[uuid.UUID]LockedTarget{}
	for _, target := range targets {
		if target.ID == uuid.Nil {
			return nil, ErrInvalid
		}
		if _, exists := targetRows[target.ID]; exists {
			return nil, ErrInvalid
		}
		targetRows[target.ID] = target
	}
	byTarget := map[uuid.UUID]PreparedShownBackgroundCleanup{}
	for _, task := range tasks {
		id := task.compiled.params.TargetID
		if id == uuid.Nil {
			return nil, ErrInvalid
		}
		if _, exists := byTarget[id]; exists {
			return nil, ErrInvalid
		}
		byTarget[id] = task
	}
	result := make([]PreparedShownBackgroundCleanup, 0, len(drafts))
	for _, draft := range drafts {
		task, exists := byTarget[draft.TargetID]
		decision := decisions[draft.TargetID]
		preview := task.preview
		p := task.compiled.params
		if !exists || draft.Kind != "background_cleanup" || preview.Kind != "background_cleanup" || decision.Decision != "approve" || decision.Preview == nil || p.ApprovedEdit != draft.Params ||
			decision.Preview.Render.ArtifactID != p.WorkingImage.ArtifactID || decision.Preview.Render.SHA256 != p.WorkingImage.SHA256 ||
			decision.Preview.Evidence.ArtifactID != p.Presentation.ArtifactID || decision.Preview.Evidence.SHA256 != p.Presentation.SHA256 ||
			p.ChildRunID != child || p.TargetID != preview.TargetID || p.TenantID != preview.TenantID ||
			preview.Plan != private.PlanRef || preview.Intent != private.IntentRef || preview.SnapshotID != private.SnapshotID || preview.Plan.SHA256 != owned.PlanSHA || preview.WorkflowRevision != owned.ExpectedRevision ||
			preview.TargetRevision != strconv.FormatInt(targetRows[draft.TargetID].Revision, 10) ||
			preview.IntentID != intentID || preview.TenantID != tenant {
			return nil, ErrInvalid
		}
		var edit ApprovedEdit
		if decodeStrict(draft.Body, &edit) != nil || preview.Source != edit.Source || preview.Intent != edit.Intent || preview.Candidate != edit.Candidate {
			return nil, ErrInvalid
		}
		frozen, digest, err := FreezeShownBackgroundCleanupAdoptionParams(p)
		if err != nil || digest != task.compiled.ParamsRef.SHA256 || !bytes.Equal(frozen, task.compiled.body) ||
			!bytes.Equal(task.payload.ParamsJCS, frozen) || len(task.documents) == 0 {
			return nil, ErrInvalid
		}
		// Own payloads before caller waits on SQL; the external task slice may change.
		task.payload.Node = append([]byte(nil), task.payload.Node...)
		task.payload.Transport = append([]byte(nil), task.payload.Transport...)
		task.payload.Inventory = append([]byte(nil), task.payload.Inventory...)
		task.payload.ParamsJCS = append([]byte(nil), task.payload.ParamsJCS...)
		task.documents = append([]byte(nil), task.documents...)
		task.compiled.body = append([]byte(nil), task.compiled.body...)
		result = append(result, task)
	}
	return result, nil
}
