package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Original saved bytes must equal the owned compiler output; never replace a
// saved Params payload to make recovery fit current declarations or defaults.
func verifyNativeInputManualParamsReplay(ctx context.Context, compiled PreparedDefectParams, ref v2input.Ref, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ref.ArtifactID == uuid.Nil || len(raw) < 2 || len(raw) > 1<<20 || len(compiled.params) < 2 || len(compiled.params) > 1<<20 {
		return v2input.ErrInvalid
	}
	sha := sha256.Sum256([]byte(compiled.params))
	if hex.EncodeToString(sha[:]) != compiled.digest || ref.SHA256 != compiled.digest || string(raw) != compiled.params {
		return v2input.ErrConflict
	}
	return nil
}
