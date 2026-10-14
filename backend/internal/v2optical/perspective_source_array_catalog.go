package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type SQLPerspectiveSourceArrayCatalog struct {
	DB     PerspectiveTransactions
	Bucket string
}

func (c SQLPerspectiveSourceArrayCatalog) ResolvePerspectiveSourceArray(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, params PerspectiveParams, sourceRaw []byte, spec PerspectiveSourceArraySpec) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Bucket == "" || len(c.Bucket) > 255 || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	specs, err := PerspectiveSourceArrays(params, sourceRaw)
	if err != nil {
		return zero, err
	}
	found := false
	for _, known := range specs {
		if known == spec {
			found = true
		}
	}
	if !found {
		return zero, v2input.ErrInvalid
	}
	base, err := canonical.Marshal(params.Base)
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
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.v29_perspective_array_source($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)", tenant, project, snapshot, who.UserID, who.SessionID, string(base), string(sourceRaw), spec.Ref.ArtifactID, spec.Ref.SHA256, spec.Role).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 16384 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var wire struct {
		ID      uuid.UUID `json:"artifact_id"`
		SHA     string    `json:"sha256"`
		Bucket  string    `json:"bucket"`
		Key     string    `json:"object_key"`
		Version string    `json:"object_version"`
		Bytes   int64     `json:"bytes"`
		Media   string    `json:"media_type"`
	}
	if json.Unmarshal(normal, &wire) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(normal, exact) || wire.ID != spec.Ref.ArtifactID || wire.SHA != spec.Ref.SHA256 || wire.Bucket != c.Bucket || wire.Key == "" || len(wire.Key) > 1024 || wire.Version == "" || wire.Version == "null" || len(wire.Version) > 1024 || wire.Bytes <= 0 {
		return zero, v2input.ErrInvalid
	}
	components := int64(spec.Channels)
	if spec.Alpha {
		components = 1
	}
	if wire.Bytes > spec.Width*spec.Height*components*4+65536 || (wire.Media != "application/x-npy" && wire.Media != "application/octet-stream") || (spec.Channels == 2 && wire.Media != "application/x-npy") {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return objectstore.Object{Key: wire.Key, VersionID: wire.Version, SHA256: wire.SHA, Bytes: wire.Bytes, ContentType: wire.Media}, nil
}
