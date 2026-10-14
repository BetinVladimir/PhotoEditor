package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type RestorationPinnedArray struct {
	Ref  v2input.Ref
	Data masknpy.PinnedMask
}

type RestorationPixelStreams struct {
	Source, Output    RestorationPinnedArray
	Target, Protected RestorationPinnedArray
	Changed           RestorationPinnedArray
	Generated         *RestorationPinnedArray
}

// VerifyRestorationResultPixels binds every physical array to the exact input
// and output documents, then compares source/output pixels in bounded memory.
// The caller must supply immutable object-version-scoped readers and close them.
func VerifyRestorationResultPixels(ctx context.Context, operation string,
	paramsRaw, sourceRaw, targetRaw, protectedRaw []byte,
	candidate RestorationReviewCandidate,
	streams RestorationPixelStreams) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var params struct {
		Base        v2input.SessionVersionBinding `json:"base"`
		Mask        v2input.Ref                   `json:"mask"`
		GlassesMask v2input.Ref                   `json:"glasses_mask"`
		Protected   v2input.Ref                   `json:"protected"`
		Mode        string                        `json:"mode"`
	}
	if json.Unmarshal(paramsRaw, &params) != nil ||
		(operation != "image.reflection" && operation != "portrait.glare") {
		return 0, v2input.ErrInvalid
	}
	generatedRequired := operation == "image.reflection" || params.Mode == "generative"
	if operation == "portrait.glare" && params.Mode != "donor" && params.Mode != "generative" ||
		generatedRequired != (candidate.GeneratedPixels != nil) {
		return 0, v2input.ErrInvalid
	}
	source, err := v2portrait.VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return 0, err
	}
	targetRef := params.Mask
	if operation == "portrait.glare" {
		targetRef = params.GlassesMask
	}
	if err = restorationExactJSON(targetRef, targetRaw); err != nil {
		return 0, err
	}
	if err = restorationExactJSON(params.Protected, protectedRaw); err != nil {
		return 0, err
	}
	target, err := review.ParseMaskRef(targetRaw)
	if err != nil {
		return 0, v2input.ErrInvalid
	}
	protected, err := review.ParseMaskRef(protectedRaw)
	if err != nil || target.Canvas != source.Canvas || protected.Canvas != source.Canvas ||
		!bytes.Equal(target.TransformChain, source.TransformChain) ||
		!bytes.Equal(protected.TransformChain, source.TransformChain) ||
		target.Protected || !protected.Protected {
		return 0, v2input.ErrInvalid
	}
	ref := func(value review.ArtifactRef) v2input.Ref {
		return v2input.Ref{ArtifactID: value.ID, SHA256: value.SHA256}
	}
	want := []struct {
		array RestorationPinnedArray
		ref   v2input.Ref
	}{
		{streams.Source, ref(source.Pixels)}, {streams.Output, candidate.Pixels},
		{streams.Target, ref(target.Artifact)}, {streams.Protected, ref(protected.Artifact)},
		{streams.Changed, candidate.ChangedPixels}}
	for _, item := range want {
		if !isDocumentRef(item.ref) || item.array.Ref != item.ref ||
			item.array.Data.SHA256 != item.ref.SHA256 || item.array.Data.Reader == nil {
			return 0, v2input.ErrInvalid
		}
	}
	var generated *masknpy.PinnedMask
	if candidate.GeneratedPixels != nil {
		if streams.Generated == nil || streams.Generated.Ref != *candidate.GeneratedPixels ||
			streams.Generated.Data.SHA256 != candidate.GeneratedPixels.SHA256 ||
			streams.Generated.Data.Reader == nil {
			return 0, v2input.ErrInvalid
		}
		generated = &streams.Generated.Data
	} else if streams.Generated != nil {
		return 0, v2input.ErrInvalid
	}
	count, err := masknpy.VerifyRestorationPixelDelta(ctx, streams.Source.Data,
		streams.Output.Data, streams.Target.Data, streams.Protected.Data,
		streams.Changed.Data, generated, source.Canvas.Width, source.Canvas.Height)
	if errors.Is(err, masknpy.ErrInvalid) {
		return 0, v2input.ErrInvalid
	}
	return count, err
}
