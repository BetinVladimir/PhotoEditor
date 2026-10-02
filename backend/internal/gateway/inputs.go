package gateway

import (
	"context"
	"crypto/x509"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
)

// Input returns the exact immutable version authorized by the dispatched task.
// Bucket is validated against deployment config and never sent to a worker.
func (a Authorizer) Input(ctx context.Context, id TaskIdentity, cert *x509.Certificate, artifact uuid.UUID, bucket string) (objectstore.Object, error) {
	var result objectstore.Object
	if !id.valid() || cert == nil || cert.IsCA || len(cert.Raw) == 0 || artifact == uuid.Nil || bucket == "" {
		return result, ErrCapability
	}
	// Check full expected identity, including tenant/run. The second SQL function
	// rechecks live authorization in the same query that selects the artifact.
	if _, err := a.Authorize(ctx, id, cert); err != nil {
		return result, err
	}
	var actualID uuid.UUID
	var actualBucket string
	err := a.DB.QueryRow(ctx, `SELECT artifact_id,bucket,object_key,object_version,sha256,bytes,media_type
 FROM photo.gateway_input($1,$2,$3,$4,$5,$6)`, id.TaskID, id.Attempt, id.Fence, id.WorkerID, certificateHash(cert), artifact).Scan(
		&actualID, &actualBucket, &result.Key, &result.VersionID, &result.SHA256, &result.Bytes, &result.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, objectstore.ErrMissing
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	if actualID != artifact || actualBucket != bucket {
		return objectstore.Object{}, objectstore.ErrMissing
	}
	return result, nil
}
