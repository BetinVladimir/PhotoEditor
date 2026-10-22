package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

type CandidatePreviewObjects struct {
	Source, Manifest, Derivative     ArtifactRef
	ManifestObject, DerivativeObject objectstore.Object
	ProfileHash                      string
}
type SQLCandidatePreviewCatalog struct {
	DB              *pgxpool.Pool
	Bucket, SRGBSHA string
}

// Lookup finds an already independently adopted render presentation receipt.
// It never infers source identity from image dimensions or artifact names.
func (s SQLCandidatePreviewCatalog) Lookup(ctx context.Context, who auth.Identity, tid, reviewID uuid.UUID, side, variant string) (CandidatePreviewObjects, error) {
	var zero CandidatePreviewObjects
	if s.DB == nil || s.Bucket == "" || (side != "before" && side != "after") || (variant != "preview1600" && variant != "preview2560") {
		return zero, domain.NewError("INPUT_INVALID", 422)
	}
	profile, err := media.ProfileHash(variant, s.SRGBSHA)
	if err != nil {
		return zero, err
	}
	legacyProfile,_:=media.LegacyProfileHash(variant,s.SRGBSHA)
	var out CandidatePreviewObjects
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		current, err := loadDecisionContext(ctx, tx, tid, reviewID, s.Bucket)
		if err != nil {
			return err
		}
		if !candidatePreviewKind(current.Kind) {
			return domain.NewError("INPUT_INVALID", 422)
		}
		if _, err = project.Authorize(ctx, tx, tid, current.ProjectID, who.UserID, role, domain.Editor, false); err != nil {
			return err
		}
		if err = requireRelightReadAuthority(ctx, tx, who, tid, reviewID, current.Kind); err != nil { return err }
		source := current.Proposal
		if side == "before" {
			source = current.Image
		}
		isRestoration := restorationPreviewKind(current.Kind)
		nodeKey := candidatePreviewNodeKey(current.Kind, side, variant)
		originColumn, receiptColumn := candidatePreviewSQLColumns(current.Kind)
		var manifestBucket, derivativeBucket string
		err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT m.id,m.sha256,m.bucket,m.object_key,m.object_version,m.bytes,m.media_type,
   a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type,
   receipt.body->'manifest'->>'profile_hash'
   FROM photo.backend_documents d JOIN photo.tasks t ON t.tenant_id=d.tenant_id AND t.id=d.owner_id AND t.run_id=d.run_id
   JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
   JOIN photo.tasks origin ON origin.tenant_id=r.tenant_id AND origin.id=%s
   JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id AND workflow.run_id=origin.run_id
   CROSS JOIN LATERAL (SELECT photo.v26_derivative_receipt_document(%s) AS body) receipt
   CROSS JOIN LATERAL jsonb_array_elements(receipt.body->'objects') mo
   CROSS JOIN LATERAL jsonb_array_elements(receipt.body->'objects') io
   JOIN photo.artifacts m ON m.tenant_id=d.tenant_id AND m.id::text=mo->>'artifact_id' AND m.sha256=mo->>'sha256'
   JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id::text=io->>'artifact_id' AND a.sha256=io->>'sha256'
   JOIN photo.output_uploads mu ON mu.tenant_id=m.tenant_id AND mu.artifact_id=m.id AND mu.task_id=t.id
   JOIN photo.output_uploads iu ON iu.tenant_id=a.tenant_id AND iu.artifact_id=a.id AND iu.task_id=t.id
   WHERE d.tenant_id=$1 AND d.purpose='task_params' AND d.state='ready'
   AND workflow.project_id=$6 AND origin.state='succeeded' AND NOT origin.cancel_requested
   AND (NOT $8::boolean OR origin.id=(SELECT review.task_id FROM photo.reviews review
    WHERE review.tenant_id=$1 AND review.id=$7 AND review.kind IN('portrait.glare','image.reflection')))
   AND t.node_key=$9
   AND (NOT $8::boolean OR (r.config_snapshot->>'source_review_id'=$7::text
    AND r.config_snapshot->>'srgb_sha256'=$10
    AND (CASE WHEN $11::text='before' THEN r.config_snapshot->'before'
     ELSE r.config_snapshot->'after' END)=jsonb_build_object('artifact_id',$2::text,'sha256',$3::text)))
   AND r.config_snapshot->>'source_result_id'=origin.canonical_result_id::text
   AND r.kind='preview' AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
   AND t.operation='media.preview' AND t.state='succeeded' AND NOT t.cancel_requested
   AND receipt.body->>'result_id'=t.canonical_result_id::text
   AND receipt.body->>'attempt'=t.attempt::text AND receipt.body->>'fence'=t.fencing_token::text
   AND receipt.body->'manifest'->'source'=jsonb_build_object('artifact_id',$2::text,'sha256',$3::text)
   AND receipt.body->'manifest'->>'variant'=$4 AND receipt.body->'manifest'->>'profile_hash'=ANY($5::text[])
   AND m.id::text=receipt.body->>'manifest_id'
   AND a.id::text=receipt.body->'manifest'->'derivative'->>'artifact_id'
   AND a.sha256=receipt.body->'manifest'->'derivative'->>'sha256'
   AND m.output_role='derivative_manifest' AND a.output_role='derivative'
   AND m.state='ready' AND a.state='ready' AND m.verified_at IS NOT NULL AND a.verified_at IS NOT NULL
   AND m.created_by_task_id=t.id AND a.created_by_task_id=t.id
   AND m.produced_attempt=t.attempt AND a.produced_attempt=t.attempt AND m.produced_fence=t.fencing_token AND a.produced_fence=t.fencing_token
   AND m.bucket=mo->>'bucket' AND m.object_key=mo->>'key' AND m.object_version=mo->>'version' AND m.bytes=(mo->>'bytes')::bigint AND m.media_type=mo->>'media_type'
   AND a.bucket=io->>'bucket' AND a.object_key=io->>'key' AND a.object_version=io->>'version' AND a.bytes=(io->>'bytes')::bigint AND a.media_type=io->>'media_type'
   AND mu.state='ready' AND iu.state='ready' AND mu.attempt=t.attempt AND iu.attempt=t.attempt AND mu.fence=t.fencing_token AND iu.fence=t.fencing_token
   AND mu.object_version=m.object_version AND iu.object_version=a.object_version
   AND mu.bucket=m.bucket AND mu.object_key=m.object_key AND mu.sha256=m.sha256 AND mu.bytes=m.bytes AND mu.media_type=m.media_type AND mu.role=m.output_role
   AND iu.bucket=a.bucket AND iu.object_key=a.object_key AND iu.sha256=a.sha256 AND iu.bytes=a.bytes AND iu.media_type=a.media_type AND iu.role=a.output_role
   AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp()) AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   ORDER BY (receipt.body->'manifest'->>'profile_hash'=($5::text[])[1]) DESC,r.created_at DESC,t.id LIMIT 1`, originColumn, receiptColumn), tid, source.ID, source.SHA256, variant, []string{profile,legacyProfile}, current.ProjectID, reviewID,
			isRestoration, nodeKey, s.SRGBSHA, side).Scan(&out.Manifest.ID, &out.Manifest.SHA256, &manifestBucket, &out.ManifestObject.Key, &out.ManifestObject.VersionID, &out.ManifestObject.Bytes, &out.ManifestObject.ContentType, &out.Derivative.ID, &out.Derivative.SHA256, &derivativeBucket, &out.DerivativeObject.Key, &out.DerivativeObject.VersionID, &out.DerivativeObject.Bytes, &out.DerivativeObject.ContentType,&out.ProfileHash)
		if errors.Is(err, pgx.ErrNoRows) && candidatePreviewAssetFallbackAllowed(current.Kind, side) {
			out, err = lookupCandidateAssetPreview(ctx, tx, tid, current.ProjectID, source, variant, profile, s.Bucket)
			if errors.Is(err,pgx.ErrNoRows) {
				out,err=lookupCandidateAssetPreview(ctx,tx,tid,current.ProjectID,source,variant,legacyProfile,s.Bucket)
			}
			return err
		}
		if err != nil {
			return err
		}
		out.Source = source
		out.ManifestObject.SHA256 = out.Manifest.SHA256
		out.DerivativeObject.SHA256 = out.Derivative.SHA256
		if err := validateCandidatePreviewObjects(out, manifestBucket, derivativeBucket, s.Bucket); err != nil {
			return err
		}

		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	return out, nil
}

func restorationPreviewKind(kind string) bool {
	return kind == "portrait.glare" || kind == "image.reflection"
}

func candidatePreviewNodeKey(kind, side, variant string) string {
	if restorationPreviewKind(kind) {
		return "restoration." + side + ":" + variant
	}
	return "render.derivative:" + variant
}

func candidatePreviewAssetFallbackAllowed(kind, side string) bool {
	return side == "before" && !restorationPreviewKind(kind)
}

func candidatePreviewSQLColumns(kind string) (origin, receipt string) {
	if restorationPreviewKind(kind) {
		return "r.v28_restoration_preview_task_id", "d.v28_restoration_preview_derivative_receipt"
	}
	return "r.v26_derivative_task_id", "d.v26_derivative_receipt"
}
