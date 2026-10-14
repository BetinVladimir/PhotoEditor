package provenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type SigningOriginalCatalog interface {
	LookupSigningOriginal(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, v2input.Ref) (objectstore.Object, error)
}
type SigningOriginalQualifier interface {
	QualifySigningOriginal(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, v2input.Ref, int64) error
}
type BoundSigningOriginalQualifier struct {
	Catalog SigningOriginalCatalog
	Objects SigningObjects
}

// Qualify hashes original bytes without retaining an image-sized buffer. This
// proves original identity, not decoder/model/photo scientific qualification.
func (s BoundSigningOriginalQualifier) QualifySigningOriginal(ctx context.Context, who auth.Identity, ready ReadySigningExport, e CanonicalSigningTask, original, master v2input.Ref, expectedBytes int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Catalog == nil || s.Objects == nil || !ref(original) || !ref(master) || original == master || expectedBytes < 1 || expectedBytes > 512<<20 || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return ErrInvalid
	}
	object, err := s.Catalog.LookupSigningOriginal(ctx, who, ready, e, original, master)
	if err != nil {
		return err
	}
	if object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != original.SHA256 || object.Bytes != expectedBytes || !text(object.ContentType, 255) || object.ContentType == "application/json" {
		return ErrInvalid
	}
	stream, err := s.Objects.Open(ctx, object)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return err
	}
	if stream == nil {
		return ErrInvalid
	}
	hash := sha256.New()
	size, readErr := io.Copy(hash, io.LimitReader(signingContextReader{ctx: ctx, reader: stream}, expectedBytes+1))
	closeErr := stream.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if size != expectedBytes || hex.EncodeToString(hash.Sum(nil)) != original.SHA256 {
		return ErrInvalid
	}
	current, err := s.Catalog.LookupSigningOriginal(ctx, who, ready, e, original, master)
	if err != nil {
		return err
	}
	if current != object {
		return ErrInvalid
	}
	return ctx.Err()
}

type SQLSigningOriginalCatalog struct {
	DB     *pgxpool.Pool
	Tasks  SigningTaskCatalog
	Bucket string
}

func (s SQLSigningOriginalCatalog) LookupSigningOriginal(ctx context.Context, who auth.Identity, ready ReadySigningExport, e CanonicalSigningTask, original, master v2input.Ref) (objectstore.Object, error) {
	var object objectstore.Object
	if err := ctx.Err(); err != nil {
		return object, err
	}
	if s.DB == nil || s.Tasks == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !ref(original) || !ref(master) || original == master || e.Operation != "asset.decode" || e.Version != "1" || normalizeCanonicalSigningTask(&e) != nil {
		return object, ErrInvalid
	}
	inputs, err := canonicalTaskInputRefs(e.Inputs)
	if err != nil || len(inputs) != 1 || inputs[0] != original {
		return object, ErrInvalid
	}
	outputs, err := canonicalTaskOutputRefs(e.Outputs)
	if err != nil || len(outputs) < 2 || len(outputs) > 3 || outputs[len(outputs)-1] != master {
		return object, ErrInvalid
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
		err := tx.QueryRow(ctx, `SELECT original.bucket,original.object_key,original.object_version,original.sha256,original.bytes,original.media_type
   FROM photo.assets asset
   JOIN photo.asset_versions version ON version.tenant_id=asset.tenant_id AND version.asset_id=asset.id
   JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND original.id=asset.original_artifact_id
   JOIN photo.artifacts working ON working.tenant_id=version.tenant_id AND working.id=version.master_artifact_id
   WHERE asset.tenant_id=$1 AND asset.project_id=$2 AND asset.deleted_at IS NULL AND asset.upload_state='verified'
   AND original.id=$3 AND original.sha256=$4 AND original.bucket=$5
   AND original.state='ready' AND original.verified_at IS NOT NULL AND original.object_version NOT IN ('','null')
   AND original.bytes BETWEEN 1 AND 536870912 AND original.media_type<>'application/json'
   AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
   AND version.master_artifact_id=$6 AND version.content_hash=$7 AND version.created_by_run_id=$8
   AND working.sha256=$7 AND working.created_by_task_id=$9 AND working.produced_attempt=$10 AND working.produced_fence=$11
   AND working.state='ready' AND working.verified_at IS NOT NULL AND working.object_version NOT IN ('','null')
   AND working.media_type='application/json' AND working.bytes BETWEEN 2 AND 1048576
   AND (working.expires_at IS NULL OR working.expires_at>clock_timestamp())
   ORDER BY version.id LIMIT 1 FOR SHARE OF asset,version,original,working`, ready.Tenant, ready.Project, original.ArtifactID, original.SHA256, s.Bucket, master.ArtifactID, master.SHA256, e.Run, e.Task, e.Attempt, e.Fence).Scan(&bucket, &object.Key, &object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType)
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

var _ SigningOriginalQualifier = BoundSigningOriginalQualifier{}
var _ SigningOriginalCatalog = SQLSigningOriginalCatalog{}
