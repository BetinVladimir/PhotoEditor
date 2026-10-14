package v2draft

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// RestorationResultArrayRefs derives the only array IDs allowed for physical
// verification from canonical input/result documents. No caller array list is
// accepted at the adoption-facing boundary.
func RestorationResultArrayRefs(ctx context.Context, operation string,
	paramsRaw, sourceRaw, targetRaw, protectedRaw []byte,
	candidate RestorationReviewCandidate) (RestorationPixelStreams, error) {
	var zero RestorationPixelStreams
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	var params struct {
		Base        v2input.SessionVersionBinding `json:"base"`
		Mask        v2input.Ref                   `json:"mask"`
		GlassesMask v2input.Ref                   `json:"glasses_mask"`
		Protected   v2input.Ref                   `json:"protected"`
	}
	if json.Unmarshal(paramsRaw, &params) != nil ||
		(operation != "portrait.glare" && operation != "image.reflection") {
		return zero, v2input.ErrInvalid
	}
	source, err := v2portrait.VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return zero, err
	}
	targetRef := params.Mask
	if operation == "portrait.glare" {
		targetRef = params.GlassesMask
	}
	if restorationExactJSON(targetRef, targetRaw) != nil ||
		restorationExactJSON(params.Protected, protectedRaw) != nil {
		return zero, v2input.ErrInvalid
	}
	target, err := review.ParseMaskRef(targetRaw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	protected, err := review.ParseMaskRef(protectedRaw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	ref := func(value review.ArtifactRef) v2input.Ref {
		return v2input.Ref{ArtifactID: value.ID, SHA256: value.SHA256}
	}
	streams := RestorationPixelStreams{
		Source:    RestorationPinnedArray{Ref: ref(source.Pixels)},
		Output:    RestorationPinnedArray{Ref: candidate.Pixels},
		Target:    RestorationPinnedArray{Ref: ref(target.Artifact)},
		Protected: RestorationPinnedArray{Ref: ref(protected.Artifact)},
		Changed:   RestorationPinnedArray{Ref: candidate.ChangedPixels},
	}
	if candidate.GeneratedPixels != nil {
		streams.Generated = &RestorationPinnedArray{Ref: *candidate.GeneratedPixels}
	}
	for _, item := range []RestorationPinnedArray{streams.Source, streams.Output,
		streams.Target, streams.Protected, streams.Changed} {
		if !isDocumentRef(item.Ref) {
			return zero, v2input.ErrInvalid
		}
	}
	if streams.Generated != nil && !isDocumentRef(streams.Generated.Ref) {
		return zero, v2input.ErrInvalid
	}
	return streams, ctx.Err()
}
