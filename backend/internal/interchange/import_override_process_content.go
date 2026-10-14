package interchange

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Process-owned source and manual-content gate. NativeImportOverrideContent
// validates complete replacement modules and installed model locks; the
// surrounding prepared qualifier independently checks every inline mask.
type ImportProcessOverrideEnvelope struct {
	Masters ImportMasterReader
	Binding v2input.SessionVersionBinding
}

func (q ImportProcessOverrideEnvelope) QualifyImportOverrideContent(ctx context.Context, raw []byte, binding v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.Masters == nil || q.Binding != binding || !validImportRef(binding.Source) || !validImportRefForSHA(binding.CanvasHash) || len(raw) < 2 || len(raw) > 8<<20 {
		return v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	normalized, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(normalized, body) {
		return v2input.ErrInvalid
	}
	read := func() ([]byte, *review.WorkingImage, error) {
		current, err := q.Masters.ReadImportMaster(ctx, binding.Source, 1<<20)
		if err != nil {
			return nil, nil, err
		}
		owned := append([]byte(nil), current...)
		if len(owned) < 2 || len(owned) > 1<<20 || rawSHA(owned) != binding.Source.SHA256 {
			return nil, nil, v2input.ErrInvalid
		}
		image, err := review.ParseWorkingImage(owned)
		if err != nil {
			return nil, nil, err
		}
		canvas, err := canonical.Hash(image.Canvas)
		if err != nil || canvas != binding.CanvasHash || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color.Encoding != "gamma24" {
			return nil, nil, v2input.ErrInvalid
		}
		return owned, &image, ctx.Err()
	}
	before, image, err := read()
	if err != nil {
		return err
	}
	if err := editor.ValidateOverride(body, image.Canvas.SourceSHA256); err != nil {
		return err
	}
	after, _, err := read()
	if err != nil {
		return err
	}
	if !bytes.Equal(after, before) {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

type ImportOverrideContentChain struct {
	First, Second ImportOverrideContentQualifier
}

func (q ImportOverrideContentChain) QualifyImportOverrideContent(ctx context.Context, raw []byte, binding v2input.SessionVersionBinding) error {
	if q.First == nil || q.Second == nil {
		return v2input.ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	if err := q.First.QualifyImportOverrideContent(ctx, append([]byte(nil), owned...), binding); err != nil {
		return err
	}
	return q.Second.QualifyImportOverrideContent(ctx, append([]byte(nil), owned...), binding)
}

var _ ImportOverrideContentQualifier = ImportProcessOverrideEnvelope{}
var _ ImportOverrideContentQualifier = ImportOverrideContentChain{}
