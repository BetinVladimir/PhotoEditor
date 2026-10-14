package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Content must validate every complete module, manual geometry/landmarks and
// portable model/asset dependency. The editor envelope validator is insufficient.
type ImportOverrideContentQualifier interface {
	QualifyImportOverrideContent(context.Context, []byte, v2input.SessionVersionBinding) error
}

type ImportPreparedOverrideQualifier struct {
	Masters   ImportMasterReader
	Masks     ImportMaskQualifier
	Content   ImportOverrideContentQualifier
	Confirmed ImportConfirmedMaskAuthority
}

func (q ImportPreparedOverrideQualifier) QualifyImportedOverride(ctx context.Context, raw []byte, binding v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.Masters == nil || q.Masks == nil || q.Content == nil || !validImportRef(binding.Source) {
		return v2input.ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	normalized, err := canonical.Bytes(owned)
	if err != nil || len(owned) < 2 || len(owned) > 8<<20 || !bytes.Equal(normalized, owned) {
		return v2input.ErrInvalid
	}
	master, err := q.Masters.ReadImportMaster(ctx, binding.Source, 1<<20)
	if err != nil {
		return err
	}
	master = append([]byte(nil), master...)
	if rawSHA(master) != binding.Source.SHA256 {
		return v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(master)
	if err != nil {
		return err
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != binding.CanvasHash || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color.Encoding != "gamma24" {
		return v2input.ErrInvalid
	}
	if err := editor.ValidateOverride(owned, image.Canvas.SourceSHA256); err != nil {
		return err
	}
	var fields struct {
		Accepted []string `json:"accepted_candidate_ids"`
		Rejected []string `json:"rejected_candidate_ids"`
		Masks    []struct {
			Mask      json.RawMessage `json:"mask"`
			Confirmed bool            `json:"confirmed"`
		} `json:"mask_refs"`
	}
	if err := json.Unmarshal(owned, &fields); err != nil {
		return err
	}
	if len(fields.Accepted)+len(fields.Rejected) > 0 {
		return &ImportRecipeUnsupported{Reason: "foreign_candidate_review_requires_remap"}
	}
	for _, entry := range fields.Masks {
		if entry.Confirmed {
			if q.Confirmed == nil {
				return &ImportRecipeUnsupported{Reason: "imported_mask_requires_new_confirmation"}
			}
			mask, err := review.ParseMaskRef(entry.Mask)
			if err != nil {
				return err
			}
			if mask.Selector != "person" || mask.ObjectID != nil {
				return &ImportRecipeUnsupported{Reason: "imported_mask_requires_new_confirmation"}
			}
			if err := q.Confirmed.QualifyPreparedMaskConfirmation(ctx, mask, binding); err != nil {
				return err
			}
		}
	}
	// Full content qualification precedes potentially expensive physical mask IO.
	if err := q.Content.QualifyImportOverrideContent(ctx, append([]byte(nil), owned...), binding); err != nil {
		return err
	}
	for _, entry := range fields.Masks {
		if err := ctx.Err(); err != nil {
			return err
		}
		mask, err := review.ParseMaskRef(entry.Mask)
		if err != nil {
			return err
		}
		if mask.Canvas != image.Canvas {
			return v2input.ErrInvalid
		}
		if err := q.Masks.QualifyImportedMask(ctx, mask, binding); err != nil {
			return err
		}
		if entry.Confirmed {
			if err := q.Confirmed.QualifyPreparedMaskConfirmation(ctx, mask, binding); err != nil {
				return err
			}
		}
	}
	current, err := q.Masters.ReadImportMaster(ctx, binding.Source, 1<<20)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, master) {
		return v2input.ErrInvalid
	}
	// The content validator must repeat current portable asset/model authority.
	if err := q.Content.QualifyImportOverrideContent(ctx, append([]byte(nil), owned...), binding); err != nil {
		return err
	}
	for _, entry := range fields.Masks {
		if entry.Confirmed {
			mask, err := review.ParseMaskRef(entry.Mask)
			if err != nil {
				return err
			}
			if err := q.Confirmed.QualifyPreparedMaskConfirmation(ctx, mask, binding); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
