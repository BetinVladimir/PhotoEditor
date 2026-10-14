package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Composes original producer/policy/source stability with exact newly published
// Params. This grants no queue authority; caller still owns task/input admission
// and rollback. Native dependencies must belong to the retained qualified set.
func (s SQLTransferSourceReader) lockPublishedDefectExecutionInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, execution defectPolicyExecutionPreparation, params defectVersionParamsProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || params.preparation.source != execution.execution.face.source.source {
		return v2input.ErrInvalid
	}
	files, err := s.decodeDefectExecutionFilePins(ctx, who, tenant, binding, execution.execution.face.source)
	if err != nil {
		return err
	}
	if len(params.preparation.params.inputs) > 4194304 {
		return v2input.ErrInvalid
	}
	var inputs []review.ArtifactRef
	if json.Unmarshal([]byte(params.preparation.params.inputs), &inputs) != nil || len(inputs) < 2 || len(inputs) > 4096 {
		return v2input.ErrInvalid
	}
	known := map[v2input.Ref]bool{}
	for _, file := range files {
		known[file.Ref] = true
	}
	seen := map[uuid.UUID]bool{}
	for _, input := range inputs {
		if seen[input.ID] || !known[v2input.Ref{ArtifactID: input.ID, SHA256: input.SHA256}] || input.ID == params.params.ArtifactID {
			return v2input.ErrInvalid
		}
		seen[input.ID] = true
	}
	// Every new retained manual root must match the full original qualified proof.
	if params.preparation.manualCount < 0 || params.preparation.manualCount > 64 || execution.execution.manualCount < 0 || execution.execution.manualCount > 64 {
		return v2input.ErrInvalid
	}
	for i := 0; i < params.preparation.manualCount; i++ {
		found := false
		for j := 0; j < execution.execution.manualCount; j++ {
			if params.preparation.manual[i] == execution.execution.manual[j] {
				found = true
				break
			}
		}
		if !found {
			return v2input.ErrInvalid
		}
	}
	if err := matchDefectExecutionPolicySnapshot(execution.execution, execution.policy); err != nil {
		return err
	}
	if err := s.lockDefectPolicyExecutionInTransaction(ctx, tx, who, tenant, binding, execution); err != nil {
		return err
	}
	if err := s.lockDefectVersionParamsProof(ctx, tx, who, tenant, binding, params); err != nil {
		return err
	}
	return s.lockDefectPolicyExecutionInTransaction(ctx, tx, who, tenant, binding, execution)
}
