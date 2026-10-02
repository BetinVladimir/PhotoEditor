package v2sessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/domain"
)

// PlanMaterializer is a scheduler-role boundary. It reads the adopted
// versioned ScenePlan object and publishes relational review rows only after
// its exact bytes and complete snapshot partition have been verified.
type PlanMaterializer struct {
	DB      *pgxpool.Pool
	Objects ObjectReader
	Bucket  string
}

func (m PlanMaterializer) ProcessOne(ctx context.Context) (bool, error) {
	if m.DB == nil {
		return false, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var tenantID, runID uuid.UUID
	err := m.DB.QueryRow(ctx, `SELECT tenant_id,run_id
		FROM photo.v22_pending_scene_plan_materializations() LIMIT 1`).
		Scan(&tenantID, &runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = m.Materialize(ctx, tenantID, runID)
	return err == nil, err
}

func (m PlanMaterializer) Materialize(ctx context.Context, tenantID, runID uuid.UUID) (uuid.UUID, error) {
	if m.DB == nil || m.Objects == nil || m.Bucket == "" {
		return uuid.Nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || runID == uuid.Nil {
		return uuid.Nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	var snapshotID uuid.UUID
	var snapshotDigest, policySHA, modelLockSHA string
	var itemCount int64
	var source sceneObject
	err := m.DB.QueryRow(ctx, `SELECT snapshot_id,snapshot_digest,item_count,
		policy_sha,model_lock_sha,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type
		FROM photo.v22_scene_plan_materialization_source($1,$2)`, tenantID, runID).
		Scan(&snapshotID, &snapshotDigest, &itemCount, &policySHA, &modelLockSHA, &source.ID,
			&source.Object.SHA256, &source.Object.Bytes, &source.Bucket, &source.Object.Key,
			&source.Object.VersionID, &source.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	if err != nil {
		return uuid.Nil, err
	}
	if !digestPattern.MatchString(modelLockSHA) || itemCount < 1 || itemCount > 5000 {
		return uuid.Nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	raw, err := m.readSceneObject(ctx, source)
	if err != nil {
		return uuid.Nil, err
	}
	plan, err := decodeScenePlan(raw, source.Object.SHA256, snapshotID,
		snapshotDigest, policySHA, int(itemCount))
	if err != nil {
		return uuid.Nil, err
	}
	bindings, err := m.frozenBindings(ctx, tenantID, runID, snapshotID,
		snapshotDigest, int(itemCount))
	if err != nil {
		return uuid.Nil, err
	}
	if err = m.verifyCorrections(ctx, tenantID, runID, plan, bindings); err != nil {
		return uuid.Nil, err
	}
	var analysisID uuid.UUID
	err = m.DB.QueryRow(ctx, `SELECT photo.materialize_v22_scene_plan(
		$1,$2,$3,$4,$5::jsonb,$6)`, tenantID, runID, source.ID,
		source.Object.SHA256, string(raw), modelLockSHA).Scan(&analysisID)
	if err != nil {
		return uuid.Nil, analysisError(err)
	}
	return analysisID, nil
}
