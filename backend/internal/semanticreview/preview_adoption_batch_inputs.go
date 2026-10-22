package semanticreview

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// ShownBatchCommitInputs is a publication-bound decision and copy-task batch.
// It cannot be passed to old rerender admission; SQL must atomically validate
// every row, quota, publication owner and receipt before creating the child.
type ShownBatchCommitInputs struct {
	Decision  DecisionProof
	Documents json.RawMessage
	Tasks     json.RawMessage
}
type shownBatchTaskInput struct {
	TargetID         uuid.UUID       `json:"target_id"`
	ParamsArtifactID uuid.UUID       `json:"params_artifact_id"`
	ParamsSHA        string          `json:"params_sha"`
	ParamsBody       string          `json:"params_body"`
	Node             json.RawMessage `json:"node"`
	Transport        json.RawMessage `json:"transport"`
	Inventory        json.RawMessage `json:"inventory"`
	Documents        json.RawMessage `json:"documents"`
}

func PrepareShownBatchCommitInputs(tenant, intent, actor, child uuid.UUID, review PreparedReview,
	targets []LockedTarget, request ApplyDecision, decision PublishedDecision, tasks []PreparedShownExposure) (ShownBatchCommitInputs, error) {
	empty := ShownBatchCommitInputs{}
	verified, err := VerifyShownExposureBatch(tenant, intent, child, review, targets, request, tasks)
	if err != nil || len(verified) == 0 {
		return empty, ErrInvalid
	}
	drafts, err := CompileApprovedTaskDrafts(child, review, targets, request)
	if err != nil {
		return empty, err
	}
	// Reuse the private-review publication proof checks, never the old task graph.
	proof, err := PrepareApprovedCommitInputs(tenant, actor, child, review, targets, request, decision, drafts)
	if err != nil {
		return empty, err
	}
	rows := make([]shownBatchTaskInput, 0, len(verified))
	for _, task := range verified {
		rows = append(rows, shownBatchTaskInput{TargetID: task.compiled.params.TargetID, ParamsArtifactID: task.compiled.ParamsRef.ArtifactID,
			ParamsSHA: task.compiled.ParamsRef.SHA256, ParamsBody: string(task.compiled.body), Node: task.payload.Node,
			Transport: task.payload.Transport, Inventory: task.payload.Inventory, Documents: task.documents})
	}
	body, err := canonical.Marshal(rows)
	if err != nil {
		return empty, ErrInvalid
	}
	return ShownBatchCommitInputs{Decision: proof.Decision, Documents: append([]byte(nil), proof.Documents...), Tasks: body}, nil
}
