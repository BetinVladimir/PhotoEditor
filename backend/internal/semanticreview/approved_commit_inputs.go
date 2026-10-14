package semanticreview

import (
	"bytes"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// ApprovedNodeBinding is the persisted child configuration binding consumed by
// worker scope admission. It does not select or qualify an operation.
type ApprovedNodeBinding struct {
	TaskID           uuid.UUID `json:"task_id"`
	TargetID         uuid.UUID `json:"target_id"`
	ParamsArtifactID uuid.UUID `json:"params_artifact_id"`
	ParamsSHA        string    `json:"params_sha"`
}

type approvedParamsDocument struct {
	TaskID     uuid.UUID `json:"task_id"`
	TargetID   uuid.UUID `json:"target_id"`
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Body       string    `json:"body"`
}

type ApprovedCommitInputs struct {
	Decision  DecisionProof
	Documents []byte
	Params    []byte
	Nodes     []ApprovedNodeBinding
}

// PrepareApprovedCommitInputs independently rebuilds exact evidence after IO,
// rejecting changed publication projections without producing partial inputs.
// The atomic SQL admission must still recheck live authority, ready artifacts,
// child namespace, qualified operations and resources before creating any run.
func PrepareApprovedCommitInputs(tenant, actor, child uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision, decision PublishedDecision, published []ApprovedTaskDraft) (ApprovedCommitInputs, error) {
	empty := ApprovedCommitInputs{}
	if tenant == uuid.Nil || actor == uuid.Nil || child == uuid.Nil || child == prepared.Sources.RunID {
		return empty, ErrInvalid
	}
	proof, err := BuildDecisionProof(prepared, targets, request)
	if err != nil {
		return empty, err
	}
	if len(proof.Approved) == 0 || decision.BatchID != decisionDocumentOwner(tenant, prepared.Sources.RunID, actor, proof.RequestSHA256) || decision.Ref.ArtifactID == uuid.Nil || decision.Ref.SHA256 != proof.SHA256 || decision.Proof.SHA256 != proof.SHA256 || decision.Proof.RequestSHA256 != proof.RequestSHA256 || !bytes.Equal(decision.Proof.Body, proof.Body) {
		return empty, ErrInvalid
	}
	drafts, err := CompileApprovedTaskDrafts(child, prepared, targets, request)
	if err != nil {
		return empty, err
	}
	if len(drafts) != len(published) {
		return empty, ErrInvalid
	}
	byTask := make(map[uuid.UUID]ApprovedTaskDraft, len(published))
	for _, draft := range published {
		if _, exists := byTask[draft.TaskID]; exists {
			return empty, ErrInvalid
		}
		byTask[draft.TaskID] = draft
	}
	rows := make([]approvedParamsDocument, 0, len(drafts))
	nodes := make([]ApprovedNodeBinding, 0, len(drafts))
	for _, draft := range drafts {
		actual, exists := byTask[draft.TaskID]
		if !exists || actual.TargetID != draft.TargetID || actual.Params != draft.Params || actual.Kind != draft.Kind || !bytes.Equal(actual.Body, draft.Body) {
			return empty, ErrInvalid
		}
		rows = append(rows, approvedParamsDocument{draft.TaskID, draft.TargetID, draft.Params.ArtifactID, draft.Params.SHA256, string(draft.Body)})
		nodes = append(nodes, ApprovedNodeBinding{draft.TaskID, draft.TargetID, draft.Params.ArtifactID, draft.Params.SHA256})
	}
	params, err := canonical.Marshal(rows)
	if err != nil {
		return empty, ErrInvalid
	}
	documents, err := EncodeInitializationDocuments(prepared)
	if err != nil {
		return empty, err
	}
	return ApprovedCommitInputs{Decision: proof, Documents: documents, Params: params, Nodes: nodes}, nil
}
