package v2sessions

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type SceneBoardFrame struct {
	Ordinal   int       `json:"ordinal"`
	VersionID uuid.UUID `json:"version_id"`
	SourceSHA string    `json:"source_sha"`
}
type SceneBoardDocument struct {
	SessionID      uuid.UUID         `json:"session_id"`
	AnalysisID     uuid.UUID         `json:"analysis_id"`
	SnapshotID     uuid.UUID         `json:"snapshot_id"`
	SnapshotDigest string            `json:"snapshot_digest"`
	PolicySHA      string            `json:"policy_sha"`
	PlanSHA        string            `json:"plan_sha"`
	Plan           json.RawMessage   `json:"plan"`
	Frames         []SceneBoardFrame `json:"frames"`
}

// GetSceneBoard reads immutable plan bytes after current tenant/project authorization.
// No object-store IO is performed while the read transaction is held.
func (s Service) GetSceneBoard(ctx context.Context, who auth.Identity, tid, analysisID uuid.UUID) (SceneBoardDocument, error) {
	out := SceneBoardDocument{AnalysisID: analysisID, Frames: []SceneBoardFrame{}}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return out, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if analysisID == uuid.Nil {
		return out, domain.NewError("NOT_FOUND", 404)
	}
	var source sceneObject
	var projectID uuid.UUID
	var count int
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		err := tx.QueryRow(ctx, `SELECT a.project_id,a.session_id,a.snapshot_id,s.digest,s.item_count,a.policy_sha,
   ar.id,ar.sha256,ar.bytes,ar.bucket,ar.object_key,ar.object_version,ar.media_type
   FROM photo.session_analyses a JOIN photo.snapshots s ON s.tenant_id=a.tenant_id AND s.id=a.snapshot_id
   JOIN photo.v2_workflows w ON w.tenant_id=a.tenant_id AND w.id=a.workflow_id
   JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
   JOIN photo.artifacts ar ON ar.tenant_id=a.tenant_id AND ar.id=a.plan_artifact_id
   WHERE a.tenant_id=$1 AND a.id=$2 AND s.kind='v2' AND s.expires_at IS NULL
   AND w.workflow_type='session' AND w.input_snapshot_id=a.snapshot_id AND w.policy_sha=a.policy_sha
   AND r.kind='v2' AND r.status IN ('completed','waiting_review') AND NOT r.cancel_requested
   AND ar.state='ready' AND ar.output_role='document' AND ar.verified_at IS NOT NULL
   AND ar.object_version IS NOT NULL AND ar.object_version NOT IN ('','null')
   AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
   AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=a.tenant_id AND l.workflow_id=w.id AND l.artifact_id=ar.id AND l.sha256=ar.sha256)`, tid, analysisID).Scan(&projectID, &out.SessionID, &out.SnapshotID, &out.SnapshotDigest, &count, &out.PolicySHA, &source.ID, &source.Object.SHA256, &source.Object.Bytes, &source.Bucket, &source.Object.Key, &source.Object.VersionID, &source.Object.ContentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if _, err = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Viewer, false); err != nil {
			return err
		}
		if count < 1 || count > 5000 {
			return domain.NewError("ANALYSIS_CONFLICT", 409)
		}
		rows, err := tx.Query(ctx, `SELECT ordinal,asset_version_id,sha256 FROM photo.snapshot_items WHERE tenant_id=$1 AND snapshot_id=$2 ORDER BY ordinal`, tid, out.SnapshotID)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[uuid.UUID]bool{}
		for rows.Next() {
			var frame SceneBoardFrame
			if err = rows.Scan(&frame.Ordinal, &frame.VersionID, &frame.SourceSHA); err != nil {
				return err
			}
			if frame.Ordinal != len(out.Frames) || frame.VersionID == uuid.Nil || seen[frame.VersionID] || !digestPattern.MatchString(frame.SourceSHA) || len(out.Frames) >= count {
				return domain.NewError("ANALYSIS_CONFLICT", 409)
			}
			seen[frame.VersionID] = true
			out.Frames = append(out.Frames, frame)
		}
		if err = rows.Err(); err != nil {
			return err
		}
		if len(out.Frames) != count {
			return domain.NewError("ANALYSIS_CONFLICT", 409)
		}
		return nil
	})
	if err != nil {
		return SceneBoardDocument{}, err
	}
	raw, err := (PlanMaterializer{Objects: s.Objects, Bucket: s.Bucket}).readSceneObject(ctx, source)
	if err != nil {
		return SceneBoardDocument{}, err
	}
	if _, err = decodeScenePlan(raw, source.Object.SHA256, out.SnapshotID, out.SnapshotDigest, out.PolicySHA, count); err != nil {
		return SceneBoardDocument{}, err
	}
	out.PlanSHA = source.Object.SHA256
	out.Plan = raw
	return out, nil
}
