package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// The loader must enforce current editor authority and bind the immutable intent.
// Repeating it after object IO prevents an expired or replaced scope from being
// used for either preview routing or a future advisory capability projection.
func readVerifiedSemanticKind(ctx context.Context, load func() (PlanSource, error), read PlanObjectReader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if load == nil || read == nil {
		return "", ErrInvalid
	}
	before, err := load()
	if err != nil {
		return "", err
	}
	if before.Bytes < 2 || before.Bytes > 1048576 || before.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(before.Ref.SHA256) || before.Version == "" || before.Version == "null" || before.MediaType != "application/json" {
		return "", ErrInvalid
	}
	raw, err := read(ctx, before)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != before.Bytes || hex.EncodeToString(sum[:]) != before.Ref.SHA256 {
		return "", ErrInvalid
	}
	parsed, err := v2input.DecodeSemanticEditIntent(raw)
	if err != nil {
		return "", ErrInvalid
	}
	after, err := load()
	if err != nil {
		return "", err
	}
	if before != after {
		return "", ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return parsed.Kind, nil
}
