package provenance

import (
	"bytes"
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type SigningReportCatalog interface {
	LookupSigningReport(context.Context, auth.Identity, ReadySigningExport, v2input.Ref) (objectstore.Object, error)
}
type BoundSigningImageReportReader struct {
	Catalog SigningReportCatalog
	Objects SigningObjects
	Who     auth.Identity
	Ready   ReadySigningExport
}

func (s BoundSigningImageReportReader) ReadSigningImageReport(ctx context.Context, reference v2input.Ref) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Catalog == nil || s.Objects == nil || !ref(reference) {
		return nil, ErrInvalid
	}
	object, err := s.Catalog.LookupSigningReport(ctx, s.Who, s.Ready, reference)
	if err != nil {
		return nil, err
	}
	if object.Key == "" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || object.ContentType != "application/json" || object.SHA256 != reference.SHA256 || object.Bytes < 2 || object.Bytes > 1<<20 {
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
	if int64(len(raw)) != object.Bytes || !pinned(raw, reference, 1<<20) {
		return nil, ErrInvalid
	}
	// Strict report parsing occurs in the DAG compiler, before following refs.
	current, err := s.Catalog.LookupSigningReport(ctx, s.Who, s.Ready, reference)
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

type SQLSigningReportCatalog struct {
	DB     *pgxpool.Pool
	Bucket string
	Tasks  SigningTaskCatalog
}

func (s SQLSigningReportCatalog) LookupSigningReport(ctx context.Context, who auth.Identity, ready ReadySigningExport, reference v2input.Ref) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Tasks == nil || len(s.Bucket) < 3 || !text(s.Bucket, 63) || !ref(reference) || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil {
		return zero, ErrInvalid
	}
	var object objectstore.Object
	var producer uuid.UUID
	var attempt int
	var fence int64
	err := tenant.WithTenant(ctx, s.DB, who, ready.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,t.id,t.attempt,t.fencing_token
   FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
   JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
   JOIN photo.v2_artifact_links report ON report.tenant_id=w.tenant_id AND report.workflow_id=w.id AND report.project_id=w.project_id
   JOIN photo.v2_artifact_links root ON root.tenant_id=w.tenant_id AND root.workflow_id=w.id AND root.project_id=w.project_id
   WHERE a.tenant_id=$1 AND a.id=$2 AND a.sha256=$3 AND a.bucket=$4 AND w.project_id=$5
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND a.media_type='application/json' AND a.output_role='report' AND a.bytes BETWEEN 2 AND 1048576
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND t.state='succeeded' AND NOT t.cancel_requested AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
   AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token AND jsonb_array_length(z.outputs)=1
   AND report.artifact_id=a.id AND report.sha256=a.sha256 AND report.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/report','UTF8')),'hex')
   AND root.ordinal=0 AND root.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
   AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.artifact_id::text,'sha256',root.sha256))
   FOR SHARE OF a,t,z`, ready.Tenant, reference.ArtifactID, reference.SHA256, s.Bucket, ready.Project).Scan(&bucket, &object.Key, &object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType, &producer, &attempt, &fence)
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
	task, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, producer)
	if err != nil {
		return zero, err
	}
	if task.Attempt != attempt || task.Fence != fence {
		return zero, ErrInvalid
	}
	repeated, err := s.Tasks.ReadCanonicalSigningTask(ctx, who, ready, producer)
	if err != nil {
		return zero, err
	}
	if repeated.Result != task.Result || repeated.Attempt != task.Attempt || repeated.Fence != task.Fence || !bytes.Equal(repeated.Outputs, task.Outputs) {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return object, nil
}

var _ SigningImageReportReader = BoundSigningImageReportReader{}
var _ SigningReportCatalog = SQLSigningReportCatalog{}
