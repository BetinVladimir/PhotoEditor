package semanticproposal

import (
	"context"

	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Verifies physical derivation only. Caller must establish exact native catalog
// authority and reread pins after IO; this helper grants no admission permission.
func (s SQLTransferSourceReader) verifyNativeSkinDerivation(ctx context.Context, params v2input.Ref, paramsBody []byte, manifest v2input.Ref, manifestBody []byte, mask review.ProtectionMask, source review.DefectSource, labels, pixels, png defectEvidenceObject) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s.Objects == nil {
		return 0, v2input.ErrInvalid
	}
	ref := func(r v2input.Ref) review.ArtifactRef { return review.ArtifactRef{ID: r.ArtifactID, SHA256: r.SHA256} }
	parsed, err := review.VerifyFaceSkinParams(paramsBody, ref(params), manifestBody, ref(manifest), mask, source)
	if err != nil {
		return 0, err
	}
	if parsed.Labels != ref(labels.ref) || mask.Artifact != ref(pixels.ref) || labels.ref.ArtifactID == pixels.ref.ArtifactID || png.ref.ArtifactID == pixels.ref.ArtifactID || png.ref.ArtifactID == labels.ref.ArtifactID {
		return 0, v2input.ErrInvalid
	}
	for _, entry := range []defectEvidenceObject{labels, pixels, png} {
		if entry.object.Bytes < 1 || entry.object.SHA256 != entry.ref.SHA256 || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" {
			return 0, v2input.ErrInvalid
		}
	}
	if labels.object.ContentType != "application/octet-stream" || pixels.object.ContentType != "application/octet-stream" || png.object.ContentType != "image/png" {
		return 0, v2input.ErrInvalid
	}
	left, err := s.Objects.Open(ctx, labels.object)
	if err != nil {
		return 0, err
	}
	right, err := s.Objects.Open(ctx, pixels.object)
	if err != nil {
		left.Close()
		return 0, err
	}
	count, verifyErr := masknpy.VerifyFaceSkinNPY(ctx, masknpy.PinnedMask{Reader: left, Bytes: labels.object.Bytes, SHA256: labels.ref.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: pixels.object.Bytes, SHA256: pixels.ref.SHA256}, source.Canvas.Width, source.Canvas.Height)
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr != nil {
		return 0, verifyErr
	}
	if leftErr != nil {
		return 0, leftErr
	}
	if rightErr != nil {
		return 0, rightErr
	}
	if err := review.VerifyFaceSkinConfidence(mask, parsed, count); err != nil {
		return 0, err
	}
	if err := s.verifyProbabilityPNG(ctx, pixels, png, source.Canvas.Width, source.Canvas.Height); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return count, nil
}
