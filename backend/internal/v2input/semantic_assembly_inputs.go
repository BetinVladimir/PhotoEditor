package v2input

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SemanticTransferResult struct {
	Ordinal   int
	TaskID    uuid.UUID
	Candidate Ref
}

// LoadSemanticAssemblyInputs reads only the complete canonically adopted set.
// A nil set means not ready or lost authority, never partial assembly permission.
func LoadSemanticAssemblyInputs(ctx context.Context, db *pgxpool.Pool, tenant, run, workflow uuid.UUID) ([]SemanticTransferResult, error) {
	if db == nil || tenant == uuid.Nil || run == uuid.Nil || workflow == uuid.Nil {
		return nil, ErrInvalid
	}
	expected, err := WorkflowID(run)
	if err != nil || expected != workflow {
		return nil, ErrInvalid
	}
	rows, err := db.Query(ctx, "SELECT ordinal,task_id,artifact_id,sha256 FROM photo.v27_semantic_assembly_inputs($1,$2)", tenant, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SemanticTransferResult
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var item SemanticTransferResult
		if err := rows.Scan(&item.Ordinal, &item.TaskID, &item.Candidate.ArtifactID, &item.Candidate.SHA256); err != nil {
			return nil, err
		}
		task, err := TaskID(workflow, "semantic.transfer", len(result))
		if err != nil || item.Ordinal != len(result) || item.TaskID != task || !validRef(item.Candidate) || seen[item.Candidate.ArtifactID] || len(result) >= 5000 {
			return nil, ErrInvalid
		}
		seen[item.Candidate.ArtifactID] = true
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
