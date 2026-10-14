package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
)

type SQLPerspectiveLinesSources struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (s SQLPerspectiveLinesSources) ResolveSource(ctx context.Context, scope PerspectiveLinesScope, snapshot uuid.UUID, who auth.Identity) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || scope.Tenant == uuid.Nil || scope.Project == uuid.Nil || scope.Run == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || scope.Base.AssetVersionID == uuid.Nil || scope.Base.Source.ArtifactID == uuid.Nil || domainDigestInvalid(scope.Base.Source.SHA256) || domainDigestInvalid(scope.Base.CanvasHash) {
		return zero, v2input.ErrInvalid
	}
	base, err := canonical.Marshal(scope.Base)
	if err != nil {
		return zero, err
	}
	var raw []byte
	err = withPerspectivePreviewDocumentTx(ctx, s.DB, scope.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.v29_perspective_source_document($1,$2,$3,$4,$5,$6::jsonb)", scope.Tenant, scope.Project, snapshot, who.UserID, who.SessionID, string(base)).Scan(&raw)
	})
	if err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 16384 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, err
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
	if err != nil || !bytes.Equal(normal, exact) || wire.ID != scope.Base.Source.ArtifactID || wire.SHA != scope.Base.Source.SHA256 || wire.Bucket != s.Bucket || len(wire.Key) < 1 || len(wire.Key) > 1024 || wire.Version == "" || wire.Version == "null" || len(wire.Version) > 1024 || wire.Bytes < 2 || wire.Bytes > v2optical.PerspectiveWorkingImageMaxBytes || wire.Media != "application/json" {
		return zero, v2input.ErrInvalid
	}
	return objectstore.Object{Key: wire.Key, VersionID: wire.Version, SHA256: wire.SHA, Bytes: wire.Bytes, ContentType: wire.Media}, nil
}
