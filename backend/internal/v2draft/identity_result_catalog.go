package v2draft

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// SQLIdentityResultCatalog uses scheduler credentials. Each lookup is scoped
// to the canonical result and exact output occurrence by the SQL function.
type SQLIdentityResultCatalog struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (c SQLIdentityResultCatalog) ResolveIdentityResultObject(ctx context.Context,
	scope IdentityResultScope, role string, ref v2input.Ref) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 ||
		scope.Tenant == uuid.Nil || scope.Task == uuid.Nil || scope.Result == uuid.Nil ||
		!isDocumentRef(scope.Root) || !isDocumentRef(ref) ||
		(role != "report" && role != "mask" && role != "pixels") {
		return zero, v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := c.DB.Begin(bounded)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", scope.Tenant.String()); err != nil {
		return zero, err
	}
	var currentRole string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&currentRole); err != nil {
		return zero, err
	}
	if currentRole != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	if err = tx.QueryRow(bounded, `SELECT photo.v23_identity_result_object($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		scope.Tenant, scope.Task, scope.Result, scope.Root.ArtifactID, scope.Root.SHA256,
		role, ref.ArtifactID, ref.SHA256, c.Bucket).Scan(&raw); err != nil {
		return zero, err
	}
	object, err := parseRestorationLocator(raw)
	if err != nil {
		return zero, err
	}
	media, maxBytes := "application/json", int64(8388608)
	minBytes := int64(2)
	if role == "pixels" {
		media, maxBytes, minBytes = "application/x-npy", 800004096, 14
	}
	if object.Key == "" || len(object.Key) > 1024 || object.VersionID == "" ||
		object.VersionID == "null" || len(object.VersionID) > 1024 ||
		object.SHA256 != ref.SHA256 || object.ContentType != media ||
		object.Bytes < minBytes || object.Bytes > maxBytes {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return object, nil
}
