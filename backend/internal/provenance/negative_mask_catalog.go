package provenance

import (
	"bytes"
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type SQLSigningNegativeMaskCatalog struct {
	DB        *pgxpool.Pool
	Bucket    string
	Adoption  SigningNegativeAdoptionReader
	Documents SigningTaskDocumentReader
}

func (s SQLSigningNegativeMaskCatalog) LookupSigningNegativeMask(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask, descriptor v2input.Ref, expected review.MaskRef) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Adoption == nil || s.Documents == nil || len(s.Bucket) < 3 || !text(s.Bucket, 63) || !ref(descriptor) || expected.Selector != "changed" {
		return zero, ErrInvalid
	}
	root, result, err := ReadSigningNegativeResult(ctx, s.Adoption, s.Documents, who, ready, task)
	if err != nil {
		return zero, err
	}
	path := "$/changed_mask"
	if result.ChangedMask != descriptor { return zero, ErrInvalid }
	imageRaw, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, task, result.WorkingImage, 1<<20)
	if err != nil {
		return zero, err
	}
	if !pinned(imageRaw, result.WorkingImage, 1<<20) {
		return zero, ErrInvalid
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return zero, ErrInvalid
	}
	mask, err := ReadSigningNegativeMask(ctx, s.Documents, who, ready, task, image, descriptor, expected.Selector)
	if err != nil {
		return zero, err
	}
	actualRaw, err := canonical.Marshal(mask)
	if err != nil {
		return zero, err
	}
	expectedRaw, err := canonical.Marshal(expected)
	if err != nil || !bytes.Equal(actualRaw, expectedRaw) {
		return zero, ErrInvalid
	}
	var object objectstore.Object
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
   FROM photo.artifacts a JOIN photo.v2_workflows w ON w.tenant_id=a.tenant_id AND w.run_id=$2 AND w.project_id=$3
   JOIN photo.v2_artifact_links p ON p.tenant_id=w.tenant_id AND p.workflow_id=w.id AND p.project_id=w.project_id AND p.artifact_id=a.id AND p.sha256=a.sha256
   JOIN photo.v2_artifact_links d ON d.tenant_id=w.tenant_id AND d.workflow_id=w.id AND d.project_id=w.project_id
   JOIN photo.v2_artifact_links root ON root.tenant_id=w.tenant_id AND root.workflow_id=w.id AND root.project_id=w.project_id
   WHERE a.tenant_id=$1 AND a.id=$4 AND a.sha256=$5 AND a.bucket=$6
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND a.media_type='application/x-npy' AND a.output_role='mask_pixels'
   AND a.bytes BETWEEN $7 AND $7+65536 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND a.created_by_task_id=$8 AND a.produced_attempt=$9 AND a.produced_fence=$10
   AND p.purpose=encode(sha256(convert_to('tasks/'||$8::text||'/'||$11||'/artifact','UTF8')),'hex')
   AND d.artifact_id=$12 AND d.sha256=$13 AND d.purpose=encode(sha256(convert_to('tasks/'||$8::text||'/'||$11,'UTF8')),'hex')
   AND root.artifact_id=$14 AND root.sha256=$15 AND root.ordinal=0
   AND root.purpose=encode(sha256(convert_to('tasks/'||$8::text||'/$','UTF8')),'hex')
   FOR SHARE OF a`, ready.Tenant, task.Run, ready.Project, mask.Artifact.ID, mask.Artifact.SHA256, s.Bucket, 4*mask.Canvas.Width*mask.Canvas.Height, task.Task, task.Attempt, task.Fence, path, descriptor.ArtifactID, descriptor.SHA256, root.ArtifactID, root.SHA256).Scan(&bucket, &object.Key, &object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType)
		if err != nil {
			return err
		}
		if bucket != s.Bucket {
			return ErrInvalid
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	repeated, err := s.Adoption.ReadSigningAdoptedNegativeRoot(ctx, who, ready, task)
	if err != nil {
		return zero, err
	}
	if repeated != root {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return object, nil
}

var _ SigningNegativeMaskCatalog = SQLSigningNegativeMaskCatalog{}
