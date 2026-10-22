package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strconv"
)

// CompileRemovalPreviewParams reconstructs proposal scope from privately owned
// canonical plan documents and a repository target row, without inventing an
// approval. Protection must be resolved from the canonical transfer producer;
// SQL admission must independently prove that root and recheck all live fences.
func CompileRemovalPreviewParams(tenant, intent, workflow uuid.UUID, prepared PreparedReview,
	target LockedTarget, request PreviewRequest, protection v2input.Ref,
	pin RemovalQualificationPin, contextPixels, featherPixels int) (RemovalPreviewParams, error) {
	empty := RemovalPreviewParams{}
	if workflow == uuid.Nil || request.Validate() != nil || !validRemovalQualification(pin) {
		return empty, ErrInvalid
	}
	owned, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA)
	if err != nil {
		return empty, err
	}
	if owned.Intent.Kind != "remove_object" || owned.Intent.EV != nil || !owned.Intent.PreservePersons {
		return empty, ErrInvalid
	}
	if target.ID == uuid.Nil || target.State != "proposed" || target.ChildRunID != nil || target.BatchID != nil ||
		target.CandidateArtifactID == nil || target.Revision < 1 ||
		request.ExpectedRevision != strconv.FormatInt(owned.Sources.Revision, 10) ||
		request.TargetRevision != strconv.FormatInt(target.Revision, 10) || request.PlanSHA != owned.PlanRef.SHA256 {
		return empty, ErrConflict
	}
	found := false
	for _, seed := range owned.Targets {
		if seed.AssetVersionID == target.AssetVersionID && seed.CandidateArtifactID == *target.CandidateArtifactID && seed.State == "proposed" {
			found = true
		}
	}
	if !found {
		return empty, ErrConflict
	}
	for _, document := range owned.documents {
		if document.Source.Contract != "TransferCandidate" || document.Source.Ref.ArtifactID != *target.CandidateArtifactID {
			continue
		}
		candidate, err := v2input.DecodeSemanticCandidate(document.Body)
		if err != nil || candidate.State != "proposed" || candidate.Mask == nil || candidate.Transform == nil ||
			candidate.Target.AssetVersionID != target.AssetVersionID || document.Source.Ref.SHA256 != request.CandidateSHA {
			return empty, ErrConflict
		}
		p := RemovalPreviewParams{ExposurePreviewParams: ExposurePreviewParams{SchemaVersion: 1, TenantID: tenant, IntentID: intent, TargetID: target.ID,
			WorkflowID: workflow, WorkflowRevision: request.ExpectedRevision, TargetRevision: request.TargetRevision,
			SnapshotID: owned.SnapshotID, Plan: owned.PlanRef, Intent: owned.IntentRef, Candidate: document.Source.Ref,
			Source: candidate.Target, Protection: protection, ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA}, Kind: "remove_object", ModelID: "big_lama", ContextPixels: contextPixels, FeatherPixels: featherPixels}
		if _, _, err := FreezeRemovalPreviewParams(p); err != nil {
			return empty, err
		}
		return p, nil
	}
	return empty, ErrConflict
}
