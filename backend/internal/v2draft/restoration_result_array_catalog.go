package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type SQLRestorationResultArrayCatalog struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (c SQLRestorationResultArrayCatalog) ResolveRestorationArray(ctx context.Context,
	scope RestorationArrayScope, role string, ref v2input.Ref) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 ||
		scope.Tenant == uuid.Nil || scope.Task == uuid.Nil || scope.Result == uuid.Nil ||
		!restorationArrayRole(role) || !isDocumentRef(ref) ||
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
	if err = tx.QueryRow(bounded, `SELECT photo.v28_restoration_result_array(
	 $1,$2,$3,$4,$5,$6,$7,$8,$9)`, scope.Tenant, scope.Task, scope.Result,
		scope.Root, scope.RootSHA, role, ref.ArtifactID, ref.SHA256, c.Bucket).Scan(&raw); err != nil {
		return zero, err
	}
	object, err := parseRestorationArrayLocator(raw, ref)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return object, nil
}

func restorationArrayRole(role string) bool {
	switch role {
	case "source_pixels", "output_pixels", "target_pixels", "protected_pixels",
		"changed_pixels", "generated_pixels":
		return true
	}
	return false
}

func parseRestorationArrayLocator(raw []byte, ref v2input.Ref) (objectstore.Object, error) {
	object, err := parseRestorationLocator(raw)
	if err != nil {
		return objectstore.Object{}, err
	}
	if !validRestorationArrayObject(object, ref) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return object, nil
}

func parseRestorationLocator(raw []byte) (objectstore.Object, error) {
	var zero objectstore.Object
	if len(raw) < 2 || len(raw) > 4096 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var wire struct {
		Key     string `json:"key"`
		Version string `json:"version"`
		SHA     string `json:"sha256"`
		Bytes   int64  `json:"bytes"`
		Media   string `json:"media_type"`
	}
	if json.Unmarshal(normal, &wire) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(normal, exact) {
		return zero, v2input.ErrInvalid
	}
	object := objectstore.Object{Key: wire.Key, VersionID: wire.Version,
		SHA256: wire.SHA, Bytes: wire.Bytes, ContentType: wire.Media}
	return object, nil
}
