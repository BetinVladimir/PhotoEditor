package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type ImportOverrideBaseStyleReader interface {
	ReadBaseStyle(context.Context, v2input.Ref) (v2input.Ref, error)
}

// The frozen render pin names the base StyleV2 even when an Override selects
// the background. Reuse its exact physical publisher only after proving that
// this Override belongs to that archived recipe and target.
type BoundImportOverrideBackgroundRemapper struct {
	Source     ImportOverrideBaseStyleReader
	OldVersion uuid.UUID
	Target     v2input.SessionVersionBinding
	Style      ImportStyleBackgroundRemapper
}

func (r BoundImportOverrideBackgroundRemapper) RemapImportedOverrideBackground(ctx context.Context, override v2input.Ref, portable string, target v2input.SessionVersionBinding) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if r.Source == nil || r.Style == nil || r.OldVersion == uuid.Nil || target != r.Target || target.AssetVersionID == r.OldVersion || portable == "" {
		return "", v2input.ErrInvalid
	}
	style, err := r.Source.ReadBaseStyle(ctx, override)
	if err != nil {
		return "", err
	}
	id, err := r.Style.RemapImportedStyleBackground(ctx, style, portable, target)
	if err != nil {
		return "", err
	}
	if id == "" || id == portable {
		return "", v2input.ErrInvalid
	}
	current, err := r.Source.ReadBaseStyle(ctx, override)
	if err != nil {
		return "", err
	}
	if current != style {
		return "", v2input.ErrInvalid
	}
	return id, ctx.Err()
}

var _ ImportOverrideBackgroundRemapper = BoundImportOverrideBackgroundRemapper{}
