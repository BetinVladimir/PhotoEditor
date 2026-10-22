package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// DecodeRetouchPreviewFaces verifies exact bytes and the complete source-bound
// geometry. Canonical producer/qualification authority belongs to the SQL catalog.
// Null skin selects the explicitly configured face68 conservative mask; it is
// not an assertion that a segmentation mask was generated or qualified.
func DecodeRetouchPreviewFaces(params RetouchPreviewParams, sourceRaw []byte, ref v2input.Ref, raw []byte) ([]RetouchPreviewFace, error) {
	if _, _, err := FreezeRetouchPreviewParams(params); err != nil {
		return nil, err
	}
	if ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) || len(raw) < 2 || len(raw) > 1048576 {
		return nil, ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, ErrInvalid
	}
	// Worker geometry loading requires the exact canonical producer document.
	// A matching SHA alone does not establish canonical serialization.
	body, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, body) {
		return nil, ErrInvalid
	}
	if _, err := v2input.ResolveSemanticSourceMetadata(params.Source, sourceRaw); err != nil {
		return nil, ErrInvalid
	}
	image, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return nil, ErrInvalid
	}
	source := review.DefectSource{Image: review.ArtifactRef{ID: params.Source.Source.ArtifactID, SHA256: params.Source.Source.SHA256}, Pixels: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain}
	if _, err := review.ParseFace68(raw, source); err == nil {
		return []RetouchPreviewFace{{Landmarks: ref, Skin: json.RawMessage("null")}}, nil
	}
	faces, err := review.ParseFaceLandmarkCollection(raw, source)
	if err != nil || len(faces) < 1 || len(faces) > 64 {
		return nil, ErrInvalid
	}
	out := make([]RetouchPreviewFace, len(faces))
	for i, face := range faces {
		id := face.ObjectID
		out[i] = RetouchPreviewFace{Landmarks: ref, ObjectID: &id, Skin: json.RawMessage("null")}
	}
	return out, nil
}
