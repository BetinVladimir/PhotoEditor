package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// Rebuilds transport references only from original ready immutable documents.
// It cannot reserve, upload, repair missing data or resume cancelled execution.
type nativeReadyTransportPublisher struct {
	db          *pgxpool.Pool
	who         auth.Identity
	tenant, run uuid.UUID
	bucket      string
}

func (p nativeReadyTransportPublisher) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.db == nil || scope.Tenant != p.tenant || scope.Run != p.run || scope.Owner == uuid.Nil || scope.AssemblyLease != uuid.Nil || len(body) < 2 || len(body) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var ref artifactjson.Ref
	var object objectstore.Object
	var bucket string
	if err := p.db.QueryRow(ctx, "SELECT artifact_id,sha256,bytes,bucket,object_key,object_version FROM photo.read_v27_native_ready_transport($1,$2,$3,$4,$5,$6,$7,$8,$9)", scope.Tenant, scope.Run, scope.Owner, scope.Purpose, p.who.UserID, p.who.SessionID, digest, len(body), p.bucket).Scan(&ref.ID, &ref.SHA256, &object.Bytes, &bucket, &object.Key, &object.VersionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, v2input.ErrExpired
		}
		return zero, err
	}
	key, err := artifactjson.DocumentKey(scope, ref.ID)
	if err != nil || ref.ID == uuid.Nil || ref.SHA256 != digest || object.Bytes != int64(len(body)) || bucket != p.bucket || object.Key != key || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return zero, v2input.ErrConflict
	}
	return ref, nil
}
