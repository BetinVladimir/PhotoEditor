package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
)

// MaterializeShownAppliedVersion promotes the exact retained WorkingImage after
// the child completes. Object loading belongs outside this SQL mutation; the
// caller supplies bytes, never an unchecked metadata projection or new render.
func MaterializeShownAppliedVersion(ctx context.Context, db artifactjson.RowQuerier, who auth.Identity, tenant, target uuid.UUID, imageRaw []byte) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	if db == nil || tenant == uuid.Nil || target == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || len(imageRaw) < 2 || len(imageRaw) > 1048576 {
		return uuid.Nil, ErrInvalid
	}
	owned := append([]byte(nil), imageRaw...)
	if _, err := review.ParseWorkingImage(owned); err != nil {
		return uuid.Nil, ErrInvalid
	}
	proof, err := LoadShownAppliedImage(ctx, db, who, tenant, target)
	if err != nil {
		return uuid.Nil, err
	}
	sum := sha256.Sum256(owned)
	if hex.EncodeToString(sum[:]) != proof.WorkingImage.SHA256 {
		return uuid.Nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	var version *uuid.UUID
	if err := db.QueryRow(ctx, "SELECT photo.materialize_semantic_shown_version($1,$2,$3,$4,$5)", tenant, target, who.UserID, who.SessionID, string(owned)).Scan(&version); err != nil {
		return uuid.Nil, err
	}
	if err := ctx.Err(); err != nil { return uuid.Nil, err }
	if version == nil || *version != uuid.NewSHA1(proof.TaskID, []byte("V27.ShownAppliedVersion")) {
		return uuid.Nil, ErrConflict
	}
	return *version, nil
}
