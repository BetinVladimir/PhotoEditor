package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type ImportBinaryCodecRouter struct{ Masks, LUT, DefectLabels ImportBinaryRemapper }

func (r ImportBinaryCodecRouter) RemapImportBinary(ctx context.Context, entry uuid.UUID, archived, staged v2input.Ref, kind string, old, target v2input.SessionVersionBinding, maxBytes int64) (v2input.Ref, int64, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, 0, err
	}
	var selected ImportBinaryRemapper
	switch kind {
	case "MaskPixels":
		selected = r.Masks
	case "AbsoluteLUTArray":
		selected = r.LUT
	case "DefectLabelMap":
		selected = r.DefectLabels
	default:
		return v2input.Ref{}, 0, &ImportRecipeUnsupported{Reason: "binary_dependency_codec_not_installed"}
	}
	if selected == nil {
		return v2input.Ref{}, 0, &ImportRecipeUnsupported{Reason: "binary_dependency_codec_not_installed"}
	}
	return selected.RemapImportBinary(ctx, entry, archived, staged, kind, old, target, maxBytes)
}

var _ ImportBinaryRemapper = ImportBinaryCodecRouter{}
