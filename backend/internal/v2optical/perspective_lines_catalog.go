package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

type SQLPerspectiveLinesCatalog struct {
	DB     PerspectiveTransactions
	Bucket string
}

func (c SQLPerspectiveLinesCatalog) ResolvePerspectiveLines(ctx context.Context, scope PerspectiveLinesScope) (PerspectiveLinesLocator, error) {
	var zero PerspectiveLinesLocator
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || scope.Tenant == uuid.Nil || scope.Project == uuid.Nil || scope.Snapshot == uuid.Nil || scope.Actor == uuid.Nil || scope.Session == uuid.Nil || scope.Base.AssetVersionID == uuid.Nil || !validPerspectiveRef(scope.Base.Source) || !validPerspectiveRef(scope.Lines) || scope.Base.Source.ArtifactID == scope.Lines.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	base, err := canonical.Marshal(scope.Base)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := c.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", scope.Tenant.String()); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.v29_perspective_lines_document($1,$2,$3,$4,$5,$6::jsonb,$7,$8)", scope.Tenant, scope.Project, scope.Snapshot, scope.Actor, scope.Session, string(base), scope.Lines.ArtifactID, scope.Lines.SHA256).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 16384 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, err
	}
	var wire perspectiveSourceLocator
	if json.Unmarshal(normal, &wire) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(normal, exact) || wire.ArtifactID != scope.Lines.ArtifactID || wire.SHA256 != scope.Lines.SHA256 || wire.Bucket != c.Bucket || wire.Key == "" || len(wire.Key) > 1024 || wire.Version == "" || wire.Version == "null" || len(wire.Version) > 1024 || wire.Bytes < 2 || wire.Bytes > 1048576 || wire.Media != "application/json" {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return PerspectiveLinesLocator{Scope: scope, Bucket: wire.Bucket, Key: wire.Key, Version: wire.Version, Bytes: wire.Bytes, Media: wire.Media}, nil
}

var _ PerspectiveLinesCatalog = SQLPerspectiveLinesCatalog{}
