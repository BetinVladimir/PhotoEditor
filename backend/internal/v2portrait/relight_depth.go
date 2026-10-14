package v2portrait

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// RelightDepthInputs verifies the depth document, not its producer or NPY data.
func RelightDepthInputs(params RelightParams, pin RelightQualificationPin, sourceRaw, depthRaw []byte) ([]v2input.Entry, error) {
	roots, err := RelightRootInputs(params, pin)
	if err != nil {
		return nil, err
	}
	source, err := VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(depthRaw)
	if hex.EncodeToString(sum[:]) != params.Depth.SHA256 {
		return nil, v2input.ErrInvalid
	}
	depth, err := review.ParseRelativeDepth(depthRaw)
	if err != nil {
		return nil, err
	}
	normalized, err := canonical.Marshal(depth)
	if err != nil || !bytes.Equal(normalized, depthRaw) || depth.Canvas != source.Canvas || !bytes.Equal(depth.TransformChain, source.TransformChain) {
		return nil, v2input.ErrInvalid
	}
	entries := []v2input.Entry{{Ref: v2input.Ref{ArtifactID: depth.Artifact.ID, SHA256: depth.Artifact.SHA256}, Contract: "NpyArray"}}
	if depth.InvalidMask != nil {
		if depth.InvalidMask.ID == depth.Artifact.ID {
			return nil, v2input.ErrInvalid
		}
		entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: depth.InvalidMask.ID, SHA256: depth.InvalidMask.SHA256}, Contract: "NpyArray"})
	}
	for _, entry := range entries {
		for _, root := range roots {
			if entry.Ref.ArtifactID == root.Ref.ArtifactID {
				return nil, v2input.ErrInvalid
			}
		}
	}
	if len(entries) == 2 && entries[0].Ref.ArtifactID.String() > entries[1].Ref.ArtifactID.String() {
		entries[0], entries[1] = entries[1], entries[0]
	}
	return entries, nil
}
