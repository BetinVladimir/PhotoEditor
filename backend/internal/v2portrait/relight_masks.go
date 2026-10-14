package v2portrait

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// RelightMaskInputs verifies exact mask documents and common source geometry.
// It does not establish original producer authority or validate array contents.
func RelightMaskInputs(params RelightParams, pin RelightQualificationPin,
	sourceRaw, subjectRaw, protectedRaw []byte) ([]v2input.Entry, error) {
	roots, err := RelightRootInputs(params, pin)
	if err != nil {
		return nil, err
	}
	source, err := VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return nil, err
	}
	rootIDs := map[string]bool{}
	for _, root := range roots {
		rootIDs[root.Ref.ArtifactID.String()] = true
	}
	result := make([]v2input.Entry, 0, 2)
	for i, raw := range [][]byte{subjectRaw, protectedRaw} {
		if len(raw) < 2 || len(raw) > 1048576 {
			return nil, v2input.ErrInvalid
		}
		ref := params.Subject
		if i == 1 {
			ref = params.Protected
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != ref.SHA256 {
			return nil, v2input.ErrInvalid
		}
		mask, err := review.ParseMaskRef(raw)
		if err != nil {
			return nil, err
		}
		normalized, err := canonical.Marshal(mask)
		if err != nil || !bytes.Equal(normalized, raw) || mask.Canvas != source.Canvas ||
			!bytes.Equal(mask.TransformChain, source.TransformChain) || mask.Protected != (i == 1) || rootIDs[mask.Artifact.ID.String()] {
			return nil, v2input.ErrInvalid
		}
		result = append(result, v2input.Entry{Ref: v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}, Contract: "NpyArray"})
	}
	if result[0].Ref.ArtifactID == result[1].Ref.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	if result[0].Ref.ArtifactID.String() > result[1].Ref.ArtifactID.String() {
		result[0], result[1] = result[1], result[0]
	}
	return result, nil
}
