package provenance

import (
	"bytes"
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type SigningTaskDocumentCatalog interface {
	LookupSigningTaskDocument(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, int) (objectstore.Object, error)
}
type BoundSigningTaskDocumentReader struct {
	Catalog SigningTaskDocumentCatalog
	Objects SigningObjects
}

func (s BoundSigningTaskDocumentReader) ReadSigningTaskDocument(ctx context.Context, who auth.Identity, ready ReadySigningExport, e CanonicalSigningTask, r v2input.Ref, max int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Catalog == nil || s.Objects == nil || !ref(r) || max < 2 || max > 8<<20 || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return nil, ErrInvalid
	}
	object, err := s.Catalog.LookupSigningTaskDocument(ctx, who, ready, e, r, max)
	if err != nil {
		return nil, err
	}
	if object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.ContentType != "application/json" || object.SHA256 != r.SHA256 || object.Bytes < 2 || object.Bytes > int64(max) {
		return nil, ErrInvalid
	}
	stream, err := s.Objects.Open(ctx, object)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return nil, err
	}
	if stream == nil {
		return nil, ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(signingContextReader{ctx: ctx, reader: stream}, object.Bytes+1))
	closeErr := stream.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(raw)) != object.Bytes || bodyDigest(raw) != r.SHA256 {
		return nil, ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, ErrInvalid
	}
	current, err := s.Catalog.LookupSigningTaskDocument(ctx, who, ready, e, r, max)
	if err != nil {
		return nil, err
	}
	if current != object {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}

type SQLSigningTaskDocumentCatalog struct {
	DB     *pgxpool.Pool
	Bucket string
	Tasks  SigningTaskCatalog
}

func (s SQLSigningTaskDocumentCatalog) LookupSigningTaskDocument(ctx context.Context, who auth.Identity, ready ReadySigningExport, e CanonicalSigningTask, r v2input.Ref, max int) (objectstore.Object, error) {
	var object objectstore.Object
	if err := ctx.Err(); err != nil {
		return object, err
	}
	if s.DB == nil || s.Tasks == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || max < 2 || max > 8<<20 || !ref(r) || ready.Tenant == uuid.Nil || e.Task == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return object, ErrInvalid
	}
	if err := normalizeCanonicalSigningTask(&e); err != nil {
		return object, err
	}
	expected, err := canonical.Marshal(e)
	if err != nil {
		return object, err
	}
	check := func() error {
		current, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, e.Task)
		if err != nil {
			return err
		}
		if normalizeCanonicalSigningTask(&current) != nil {
			return ErrInvalid
		}
		raw, err := canonical.Marshal(current)
		if err != nil || !bytes.Equal(raw, expected) {
			return ErrInvalid
		}
		return nil
	}
	if err := check(); err != nil {
		return object, err
	}
	err = tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
   FROM photo.artifacts a
   WHERE a.tenant_id=$1 AND a.id=$2 AND a.sha256=$3 AND a.bucket=$4
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN ('','null')
   AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND $5
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND ((a.id=$6 AND a.sha256=$7)
    OR $8::jsonb @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
    OR ($9::jsonb @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
      AND a.created_by_task_id=$10 AND a.produced_attempt=$11 AND a.produced_fence=$12)
    OR ($13 IN('render.compose','optical.negative') AND a.created_by_task_id=$10 AND a.produced_attempt=$11 AND a.produced_fence=$12
      AND a.output_role IN('document','working_image','mask','report') AND jsonb_array_length($9::jsonb)=1
      AND EXISTS(SELECT 1 FROM photo.v2_workflows w
       JOIN photo.v2_artifact_links child ON child.tenant_id=w.tenant_id AND child.workflow_id=w.id AND child.project_id=w.project_id
       JOIN photo.v2_artifact_links root ON root.tenant_id=w.tenant_id AND root.workflow_id=w.id AND root.project_id=w.project_id
       WHERE w.tenant_id=$1 AND w.run_id=$15 AND w.project_id=$14
       AND child.artifact_id=a.id AND child.sha256=a.sha256
       AND ($13<>'optical.negative' OR child.purpose<>encode(sha256(convert_to('tasks/'||$10::text||'/$/generated_mask','UTF8')),'hex'))
       AND child.purpose IN(encode(sha256(convert_to('tasks/'||$10::text||'/$/working_image','UTF8')),'hex'),
         encode(sha256(convert_to('tasks/'||$10::text||'/$/report','UTF8')),'hex'),
         encode(sha256(convert_to('tasks/'||$10::text||'/$/changed_mask','UTF8')),'hex'),
         encode(sha256(convert_to('tasks/'||$10::text||'/$/generated_mask','UTF8')),'hex'))
       AND root.ordinal=0 AND root.purpose=encode(sha256(convert_to('tasks/'||$10::text||'/$','UTF8')),'hex')
       AND root.artifact_id=($9::jsonb->0->>'artifact_id')::uuid AND root.sha256=$9::jsonb->0->>'sha256'))
    OR ($13 IN ('render.compose','optical.negative') AND EXISTS(SELECT 1 FROM photo.v2_task_inputs i
      JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id AND w.run_id=$15 AND w.project_id=i.project_id
      WHERE i.tenant_id=$1 AND i.task_id=$10 AND i.project_id=$14 AND w.run_id=$15
      AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='InputClosureChunk')))

   FOR SHARE OF a`, ready.Tenant, r.ArtifactID, r.SHA256, s.Bucket, max, e.Params.ArtifactID, e.Params.SHA256, e.Inputs, e.Outputs, e.Task, e.Attempt, e.Fence, e.Operation, ready.Project, e.Run).Scan(&bucket, &object.Key, &object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType)
		if err != nil {
			return err
		}
		if bucket != s.Bucket || object.Key == "" {
			return ErrInvalid
		}
		return nil
	})
	if err != nil {
		return objectstore.Object{}, err
	}
	if err := check(); err != nil {
		return objectstore.Object{}, err
	}
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	return object, nil
}

var _ SigningTaskDocumentReader = BoundSigningTaskDocumentReader{}
var _ SigningTaskDocumentCatalog = SQLSigningTaskDocumentCatalog{}
