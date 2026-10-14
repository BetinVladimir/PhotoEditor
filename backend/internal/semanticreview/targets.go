package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type LockedTarget struct {
	ID                  uuid.UUID
	AssetVersionID      uuid.UUID
	CandidateArtifactID *uuid.UUID
	State               string
	Revision            int64
	ChildRunID          *uuid.UUID
	BatchID             *uuid.UUID
}

// LoadLockedTargets holds source and target locks until the caller transaction
// ends. These rows are server-owned admission facts, never client decisions.
func LoadLockedTargets(ctx context.Context, tx pgx.Tx, tenant, workflow, actor, session uuid.UUID, revision int64) ([]LockedTarget, error) {
	if tx == nil || tenant == uuid.Nil || workflow == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || revision < 1 {
		return nil, ErrInvalid
	}
	rows, err := tx.Query(ctx, `SELECT target_id,asset_version_id,candidate_artifact_id,state,target_revision,child_run_id,batch_id FROM photo.lock_semantic_decision_targets($1,$2,$3,$4,$5)`, tenant, workflow, actor, session, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]LockedTarget, 0)
	seen := make(map[uuid.UUID]bool)
	for rows.Next() {
		var target LockedTarget
		if err = rows.Scan(&target.ID, &target.AssetVersionID, &target.CandidateArtifactID, &target.State, &target.Revision, &target.ChildRunID, &target.BatchID); err != nil {
			return nil, err
		}
		if len(result) >= 5000 || target.ID == uuid.Nil || target.AssetVersionID == uuid.Nil || target.Revision < 1 || seen[target.ID] || !validTargetState(target.State) {
			return nil, ErrInvalid
		}
		seen[target.ID] = true
		result = append(result, target)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, ErrConflict
	}
	return result, nil
}
