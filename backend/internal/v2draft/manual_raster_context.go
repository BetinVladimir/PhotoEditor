package v2draft

import (
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// CompileManualRasterVectors binds original document bytes, not SQL authority.
// Callers must resolve each ref from canonical producer lineage and repeat live
// admission before graph sealing. This does not verify binary raster pixels.
func CompileManualRasterVectors(sourceRef v2input.Ref, sourceRaw []byte,
	rootRef v2input.Ref, rootRaw []byte, paramsRef v2input.Ref, paramsRaw []byte,
	vectorsRaw []byte) (ManualRasterVectors, error) {
	var zero ManualRasterVectors
	if !isDocumentRef(sourceRef) || len(sourceRaw) < 2 || len(sourceRaw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	sum := sha256.Sum256(sourceRaw)
	if hex.EncodeToString(sum[:]) != sourceRef.SHA256 {
		return zero, v2input.ErrInvalid
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	root, err := review.ParseManualRasterDocument(rootRaw, review.ArtifactRef{ID: rootRef.ArtifactID, SHA256: rootRef.SHA256})
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	external, err := review.VerifyManualMaskParams(paramsRaw,
		review.ArtifactRef{ID: paramsRef.ArtifactID, SHA256: paramsRef.SHA256}, rootRaw,
		review.ArtifactRef{ID: rootRef.ArtifactID, SHA256: rootRef.SHA256}, root.Target,
		source.Source(review.ArtifactRef{ID: sourceRef.ArtifactID, SHA256: sourceRef.SHA256}))
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	if external == nil {
		if vectorsRaw != nil {
			return zero, v2input.ErrInvalid
		}
		return ManualRasterVectors{Ref: paramsRef, Contract: "Params"}, nil
	}
	if err := review.VerifyManualVectorsDocument(vectorsRaw, *external,
		source.Source(review.ArtifactRef{ID: sourceRef.ArtifactID, SHA256: sourceRef.SHA256})); err != nil {
		return zero, v2input.ErrInvalid
	}
	return ManualRasterVectors{Ref: v2input.Ref{ArtifactID: external.ID, SHA256: external.SHA256}, Contract: "ManualMask"}, nil
}
