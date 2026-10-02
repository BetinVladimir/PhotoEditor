package v2draft

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type PinnedFrameDescriptor struct {
	AnalysisID uuid.UUID
	Features   FrameFeatures
	ShardRef   v2input.Ref
	Ordinal    int
}

// ReadFrameDescriptor selects a canonical adopted descriptor from the latest
// accepted analysis containing this exact asset version. A session correction
// pins the analysis by its plan artifact; otherwise the newest matching
// analysis is used. The returned object and frame binding are hash-verified.
func (s Service) ReadFrameDescriptor(ctx context.Context, who auth.Identity,
	tenantID, projectID uuid.UUID, binding v2input.SessionVersionBinding,
	modelLock string, session *ResolvedCorrection) (PinnedFrameDescriptor, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" ||
		projectID == uuid.Nil || binding.AssetVersionID == uuid.Nil ||
		!isDocumentRef(binding.Source) || !correctionDigest(modelLock) {
		return PinnedFrameDescriptor{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var analysisID, runID uuid.UUID
	var snapshotDigest string
	var shards []descriptorObject
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, role domain.Role) error {
			if _, e := project.Authorize(ctx, tx, tenantID, projectID,
				who.UserID, role, domain.Editor, false); e != nil {
				return e
			}
			var planID any
			var planSHA any
			if session != nil {
				planID, planSHA = session.Ref.ArtifactID, session.Ref.SHA256
			}
			e := tx.QueryRow(ctx, `SELECT sa.id,w.run_id,
			 r.config_snapshot->'v22_session_plan'->>'snapshot_digest'
			 FROM photo.session_analyses sa
			 JOIN photo.v2_workflows w ON w.tenant_id=sa.tenant_id
			  AND w.project_id=sa.project_id AND w.id=sa.workflow_id
			 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
			 JOIN photo.artifacts plan ON plan.tenant_id=sa.tenant_id
			  AND plan.id=sa.plan_artifact_id
			 WHERE sa.tenant_id=$1 AND sa.project_id=$2
			  AND sa.model_lock_sha=$3 AND w.workflow_type='session'
			  AND w.renderer_version='v2.2'
			  AND r.status IN ('waiting_review','completed')
			  AND plan.state='ready' AND plan.verified_at IS NOT NULL
			  AND ($4::uuid IS NULL OR EXISTS(
			   SELECT 1 FROM photo.v2_artifact_links correction
			   WHERE correction.tenant_id=sa.tenant_id
			    AND correction.workflow_id=w.id AND correction.artifact_id=$4
			    AND correction.sha256=$5))
			  AND EXISTS(SELECT 1 FROM photo.scene_groups group_row
			   JOIN photo.scene_members member ON member.tenant_id=group_row.tenant_id
			    AND member.project_id=group_row.project_id AND member.group_id=group_row.id
			   WHERE group_row.tenant_id=sa.tenant_id
			    AND group_row.project_id=sa.project_id AND group_row.analysis_id=sa.id
			    AND member.asset_version_id=$6)
			 ORDER BY sa.created_at DESC,sa.id DESC LIMIT 1`,
				tenantID, projectID, modelLock, planID, planSHA,
				binding.AssetVersionID).Scan(&analysisID, &runID, &snapshotDigest)
			if errors.Is(e, pgx.ErrNoRows) {
				return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
			}
			if e != nil || !correctionDigest(snapshotDigest) {
				return eOrInvalid(e)
			}
			rows, e := tx.Query(ctx, `SELECT a.id,a.sha256,a.bytes,a.bucket,
			 a.object_key,a.object_version,a.media_type
			 FROM photo.tasks task
			 JOIN photo.task_results result ON result.tenant_id=task.tenant_id
			  AND result.task_id=task.id AND result.id=task.canonical_result_id
			  AND result.attempt=task.attempt
			  AND result.fencing_token=task.fencing_token
			 JOIN photo.artifacts a ON a.tenant_id=task.tenant_id
			  AND a.id=(result.outputs->0->>'artifact_id')::uuid
			  AND a.sha256=result.outputs->0->>'sha256'
			 WHERE task.tenant_id=$1 AND task.run_id=$2
			  AND task.operation='session.describe' AND task.state='succeeded'
			  AND jsonb_typeof(result.outputs)='array'
			  AND jsonb_array_length(result.outputs)=1
			  AND a.state='ready' AND a.output_role='document'
			  AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
			  AND a.object_version NOT IN('','null')
			  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
			  AND a.created_by_task_id=task.id
			  AND a.produced_attempt=task.attempt
			  AND a.produced_fence=task.fencing_token
			 ORDER BY task.node_key`, tenantID, runID)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var item descriptorObject
				if e = rows.Scan(&item.Ref.ArtifactID, &item.Ref.SHA256,
					&item.Object.Bytes, &item.Bucket, &item.Object.Key,
					&item.Object.VersionID, &item.Object.ContentType); e != nil {
					return e
				}
				shards = append(shards, item)
			}
			return rows.Err()
		})
	if err != nil {
		return PinnedFrameDescriptor{}, draftMutationError(err)
	}
	if analysisID == uuid.Nil || runID == uuid.Nil ||
		len(shards) < 1 || len(shards) > 40 {
		return PinnedFrameDescriptor{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var found *PinnedFrameDescriptor
	for _, item := range shards {
		if item.Bucket != s.Bucket || !isDocumentRef(item.Ref) {
			return PinnedFrameDescriptor{}, domain.NewError("DOCUMENT_INVALID", 422)
		}
		item.Object.SHA256 = item.Ref.SHA256
		body, e := readJSONArtifact(ctx, s.Objects, item.Object, item.Ref)
		if e != nil {
			return PinnedFrameDescriptor{}, draftMutationError(e)
		}
		frames, e := descriptorFrames(body, snapshotDigest)
		if e != nil {
			return PinnedFrameDescriptor{}, domain.NewError("DOCUMENT_INVALID", 422)
		}
		for _, frame := range frames {
			var header struct {
				Binding v2input.SessionVersionBinding `json:"binding"`
				Ordinal int                           `json:"ordinal"`
			}
			if json.Unmarshal(frame, &header) != nil ||
				header.Binding != binding {
				continue
			}
			features, e := DecodeFrameFeatures(frame, binding)
			if e != nil || found != nil {
				return PinnedFrameDescriptor{}, domain.NewError("DOCUMENT_INVALID", 422)
			}
			found = &PinnedFrameDescriptor{Features: features,
				AnalysisID: analysisID, ShardRef: item.Ref,
				Ordinal: header.Ordinal}
		}
	}
	if found == nil {
		return PinnedFrameDescriptor{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return *found, nil
}

type descriptorObject struct {
	Ref    v2input.Ref
	Object objectstore.Object
	Bucket string
}

func descriptorFrames(body []byte, snapshotDigest string) ([]json.RawMessage, error) {
	fields, ok := exactFields(body, "schema_version", "snapshot_digest", "frames")
	if !ok {
		return nil, v2input.ErrInvalid
	}
	var version int
	var digest string
	var frames []json.RawMessage
	if json.Unmarshal(fields["schema_version"], &version) != nil || version != 1 ||
		json.Unmarshal(fields["snapshot_digest"], &digest) != nil ||
		digest != snapshotDigest ||
		json.Unmarshal(fields["frames"], &frames) != nil ||
		len(frames) < 1 || len(frames) > 128 {
		return nil, v2input.ErrInvalid
	}
	return frames, nil
}

func eOrInvalid(err error) error {
	if err != nil {
		return err
	}
	return v2input.ErrInvalid
}
