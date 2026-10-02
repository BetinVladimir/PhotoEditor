package v2region

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type RegionSourceQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type RegionSourceMetadata struct {
	ProjectID uuid.UUID
	Ref       v2input.Ref
	Object    objectstore.Object
}

func ReadRegionSource(ctx context.Context, db RegionSourceQuery, bucket string, tenant, asset, version, actor, session uuid.UUID) (RegionSourceMetadata, error) {
	var out RegionSourceMetadata
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if db == nil || bucket == "" || tenant == uuid.Nil || asset == uuid.Nil || version == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return out, ErrRegionInvalid
	}
	return readRegionMetadata(ctx, db.QueryRow(ctx, "SELECT * FROM photo.v26_region_source($1,$2,$3,$4,$5)", tenant, asset, version, actor, session), bucket)
}

func readRegionMetadata(ctx context.Context, row pgx.Row, bucket string) (RegionSourceMetadata, error) {
	var out RegionSourceMetadata
	var actualBucket string
	err := row.Scan(&out.ProjectID, &out.Ref.ArtifactID, &out.Ref.SHA256, &actualBucket, &out.Object.Key, &out.Object.VersionID, &out.Object.Bytes, &out.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrRegionInvalid
	}
	if err != nil {
		return out, err
	}
	digest, e := hex.DecodeString(out.Ref.SHA256)
	if e != nil || len(digest) != 32 || hex.EncodeToString(digest) != out.Ref.SHA256 || out.ProjectID == uuid.Nil || out.Ref.ArtifactID == uuid.Nil || actualBucket != bucket || out.Object.Key == "" || out.Object.VersionID == "" || out.Object.VersionID == "null" || out.Object.Bytes < 2 || out.Object.Bytes > 1048576 || out.Object.ContentType != "application/json" {
		return RegionSourceMetadata{}, ErrRegionInvalid
	}
	if err = ctx.Err(); err != nil {
		return RegionSourceMetadata{}, err
	}
	out.Object.SHA256 = out.Ref.SHA256
	return out, nil
}
