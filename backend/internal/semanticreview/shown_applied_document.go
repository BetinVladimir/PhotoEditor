package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
)

// ReadShownAppliedDocument performs version-pinned object IO between two
// authenticated reads. It must not run inside the materialization transaction.
func ReadShownAppliedDocument(ctx context.Context, db artifactjson.RowQuerier, who auth.Identity, tenant, target uuid.UUID, bucket string, read PlanObjectReader) (PlanDocument, error) {
	empty := PlanDocument{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || read == nil || bucket == "" || tenant == uuid.Nil || target == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return empty, ErrInvalid
	}
	load := func() (PlanSource, error) {
		var source PlanSource
		err := db.QueryRow(ctx, "SELECT contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.semantic_shown_applied_object($1,$2,$3,$4)", tenant, target, who.UserID, who.SessionID).Scan(&source.Contract, &source.Ref.ArtifactID, &source.Ref.SHA256, &source.Bytes, &source.Bucket, &source.Key, &source.Version, &source.MediaType)
		if errors.Is(err, pgx.ErrNoRows) {
			return PlanSource{}, ErrConflict
		}
		if err != nil {
			return PlanSource{}, err
		}
		if source.Contract != "WorkingImage" || source.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(source.Ref.SHA256) || source.Bytes < 2 || source.Bytes > 1048576 || source.Bucket != bucket || source.Key == "" || source.Version == "" || source.Version == "null" || source.MediaType != "application/json" {
			return PlanSource{}, ErrInvalid
		}
		return source, nil
	}
	before, err := load()
	if err != nil {
		return empty, err
	}
	raw, err := read(ctx, before)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if int64(len(raw)) != before.Bytes {
		return empty, ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	sum := sha256.Sum256(owned)
	if hex.EncodeToString(sum[:]) != before.Ref.SHA256 {
		return empty, ErrInvalid
	}
	if _, err := review.ParseWorkingImage(owned); err != nil {
		return empty, ErrInvalid
	}
	after, err := load()
	if err != nil {
		return empty, err
	}
	if before != after {
		return empty, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return PlanDocument{Source: before, Body: owned}, nil
}
