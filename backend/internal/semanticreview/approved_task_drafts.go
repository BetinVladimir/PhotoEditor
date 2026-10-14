package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ApprovedTaskDraft is an owned publication input. Params is a deterministic
// intended artifact identity, not evidence that the artifact exists or is ready.
// Admission must publish Body, qualify the operation and atomically bind this
// node to the committed review batch and Apply child before dispatch.
type ApprovedTaskDraft struct {
	TaskID   uuid.UUID   `json:"task_id"`
	TargetID uuid.UUID   `json:"target_id"`
	Params   v2input.Ref `json:"params"`
	Body     []byte      `json:"-"`
	Kind     string      `json:"kind"`
}

// CompileApprovedTaskDrafts derives exact task and Params identities from the
// child and locked target. It retains all semantic edit kinds without inventing
// an operation implementation or substituting legacy scalar overrides.
func CompileApprovedTaskDrafts(child uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision) ([]ApprovedTaskDraft, error) {
	if child == uuid.Nil {
		return nil, ErrInvalid
	}
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil {
		return nil, err
	}
	drafts := make([]ApprovedTaskDraft, 0, len(edits))
	for _, edit := range edits {
		body, err := canonical.Marshal(edit)
		if err != nil || len(body) > 1048576 {
			return nil, ErrInvalid
		}
		task := uuid.NewSHA1(child, []byte("semantic.edit:"+edit.TargetID.String()))
		params := uuid.NewSHA1(task, []byte("Params"))
		digest := sha256.Sum256(body)
		drafts = append(drafts, ApprovedTaskDraft{TaskID: task, TargetID: edit.TargetID,
			Params: v2input.Ref{ArtifactID: params, SHA256: hex.EncodeToString(digest[:])}, Body: body, Kind: edit.Kind})
	}
	return drafts, nil
}
