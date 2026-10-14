package v2draft

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type SQLRestorationResultDocumentCatalog struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (c SQLRestorationResultDocumentCatalog) ResolveRestorationDocument(ctx context.Context,
	scope RestorationArrayScope, role string, ref v2input.Ref) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 ||
		scope.Tenant == uuid.Nil || scope.Task == uuid.Nil || scope.Result == uuid.Nil ||
		!restorationDocumentRole(role) || !isDocumentRef(ref) ||
		!isDocumentRef(v2input.Ref{ArtifactID: scope.Root, SHA256: scope.RootSHA}) {
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
	if err = tx.QueryRow(bounded, `SELECT photo.v28_restoration_result_document(
	 $1,$2,$3,$4,$5,$6,$7,$8,$9)`, scope.Tenant, scope.Task, scope.Result,
		scope.Root, scope.RootSHA, role, ref.ArtifactID, ref.SHA256, c.Bucket).Scan(&raw); err != nil {
		return zero, err
	}
	object, err := parseRestorationDocumentLocator(raw, ref)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return object, nil
}

func restorationDocumentRole(role string) bool {
	switch role {
	case "source", "result", "image", "report", "changed", "generated":
		return true
	}
	return false
}

func parseRestorationDocumentLocator(raw []byte, ref v2input.Ref) (objectstore.Object, error) {
	object, err := parseRestorationLocator(raw)
	if err != nil {
		return objectstore.Object{}, err
	}
	if object.Key == "" || len(object.Key) > 1024 || object.VersionID == "" ||
		object.VersionID == "null" || len(object.VersionID) > 1024 ||
		object.SHA256 != ref.SHA256 || object.ContentType != "application/json" ||
		object.Bytes < 2 || object.Bytes > 1048576 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return object, nil
}
