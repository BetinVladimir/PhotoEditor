package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type sourceQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PlanSource struct {
	Contract  string
	Ref       v2input.Ref
	Bytes     int64
	Bucket    string
	Key       string
	Version   string
	MediaType string
}
type PlanSources struct {
	RunID    uuid.UUID
	Revision int64
	TaskID   uuid.UUID
	ResultID uuid.UUID
	Objects  []PlanSource
}

// LoadLockedPlanSources requires a live transaction. Lock acquisition and source
// read use separate statements so a wait cannot retain an old statement snapshot.
// The caller must compare this result to PreparedReview.Sources before writing.
// Deprecated: use LoadLockedPreparedSources for persistence; this variant does
// not include an intent reference outside the target snapshot.
func LoadLockedPlanSources(ctx context.Context, tx pgx.Tx, tenant, workflow, actor, session uuid.UUID, revision int64) (PlanSources, error) {
	if tx == nil || tenant == uuid.Nil || workflow == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || revision < 1 {
		return PlanSources{}, ErrInvalid
	}
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_review_sources($1,$2,$3,$4,$5)", tenant, workflow, actor, session, revision).Scan(&locked); err != nil {
		return PlanSources{}, err
	}
	if !locked {
		return PlanSources{}, ErrConflict
	}
	return LoadPlanSources(ctx, tx, tenant, workflow, actor, session)
}

// LoadLockedPreparedSources additionally locks the authenticated intent reference,
// which may lie outside the target snapshot. Derive it from private proof bytes.
func LoadLockedPreparedSources(ctx context.Context, tx pgx.Tx, tenant, workflow, actor, session uuid.UUID, prepared PreparedReview) (PlanSources, error) {
	if tx == nil || tenant == uuid.Nil || workflow == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return PlanSources{}, ErrInvalid
	}
	owned, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA)
	if err != nil {
		return PlanSources{}, err
	}
	var locked bool
	if err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_review_sources($1,$2,$3,$4,$5,$6)", tenant, workflow, actor, session, owned.Sources.Revision, owned.Intent.Reference.AssetVersionID).Scan(&locked); err != nil {
		return PlanSources{}, err
	}
	if !locked {
		return PlanSources{}, ErrConflict
	}
	current, err := LoadPlanSources(ctx, tx, tenant, workflow, actor, session)
	if err != nil {
		return PlanSources{}, err
	}
	if !EqualPlanSources(owned.Sources, current) {
		return PlanSources{}, ErrConflict
	}
	return current, nil
}

// LoadPlanSources must run inside the API authorization transaction. Object I/O
// happens afterwards; callers must re-read this inventory before committing.
func LoadPlanSources(ctx context.Context, db sourceQuerier, tenant, workflow, actor, session uuid.UUID) (PlanSources, error) {
	var empty PlanSources
	if db == nil || tenant == uuid.Nil || workflow == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return empty, ErrInvalid
	}
	rows, err := db.Query(ctx, `SELECT run_id,revision,task_id,result_id,contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.semantic_review_plan_sources($1,$2,$3,$4)`, tenant, workflow, actor, session)
	if err != nil {
		return empty, err
	}
	defer rows.Close()
	var result PlanSources
	for rows.Next() {
		var run, task, canonicalResult uuid.UUID
		var revision int64
		var source PlanSource
		if err = rows.Scan(&run, &revision, &task, &canonicalResult, &source.Contract, &source.Ref.ArtifactID, &source.Ref.SHA256, &source.Bytes, &source.Bucket, &source.Key, &source.Version, &source.MediaType); err != nil {
			return empty, err
		}
		if len(result.Objects) == 0 {
			result.RunID = run
			result.Revision = revision
			result.TaskID = task
			result.ResultID = canonicalResult
		}
		if run != result.RunID || revision != result.Revision || task != result.TaskID || canonicalResult != result.ResultID || len(result.Objects) >= 5005 {
			return empty, ErrConflict
		}
		result.Objects = append(result.Objects, source)
	}
	if err = rows.Err(); err != nil {
		return empty, err
	}
	if err = ValidatePlanSources(result); err != nil {
		return empty, err
	}
	return result, nil
}

// A partial inventory is never usable, even when the remaining objects are ready.
func ValidatePlanSources(sources PlanSources) error {
	if sources.RunID == uuid.Nil || sources.TaskID == uuid.Nil || sources.ResultID == uuid.Nil || sources.Revision < 1 || len(sources.Objects) < 6 || len(sources.Objects) > 5005 {
		return ErrConflict
	}
	counts := map[string]int{}
	seen := map[uuid.UUID]bool{}
	for _, source := range sources.Objects {
		if source.Ref.ArtifactID == uuid.Nil || seen[source.Ref.ArtifactID] || !shaPattern.MatchString(source.Ref.SHA256) || source.Bytes < 2 || source.Bytes > 8<<20 || source.Bucket == "" || source.Key == "" || source.Version == "" || source.Version == "null" || source.MediaType != "application/json" {
			return ErrConflict
		}
		switch source.Contract {
		case "SemanticPlan", "Params", "InputSnapshot", "EditIntent", "Calibration", "TransferCandidate":
		default:
			return ErrConflict
		}
		seen[source.Ref.ArtifactID] = true
		counts[source.Contract]++
	}
	for _, contract := range []string{"SemanticPlan", "Params", "InputSnapshot", "EditIntent", "Calibration"} {
		if counts[contract] != 1 {
			return ErrConflict
		}
	}
	if counts["TransferCandidate"] < 1 || counts["TransferCandidate"] > 5000 {
		return ErrConflict
	}
	return nil
}
