package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

type Landmark struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Confidence float64 `json:"confidence"`
}
type Face68 struct {
	ObjectID       uuid.UUID       `json:"object_id"`
	Points         []Landmark      `json:"points"`
	Canvas         Canvas          `json:"canvas"`
	TransformChain json.RawMessage `json:"transform_chain"`
}
type faceLandmarksDocument struct {
	Image           ArtifactRef `json:"image"`
	Faces           ArtifactRef `json:"faces"`
	Landmarks       []Face68    `json:"landmarks"`
	ModelID         string      `json:"model_id"`
	ModelLockID     string      `json:"model_lock_id"`
	Backend         string      `json:"backend"`
	PreprocessingID string      `json:"preprocessing_id"`
	MeanConfidence  float64     `json:"mean_confidence"`
	Warnings        []string    `json:"warnings"`
}

func unitLandmark(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }
func faceMatchesSource(face Face68, source DefectSource) bool {
	if face.ObjectID == uuid.Nil || len(face.Points) != 68 || !validCanvas(face.Canvas) || face.Canvas != source.Canvas || validateTransformChain(face.TransformChain, face.Canvas) != nil {
		return false
	}
	a, err := canonical.Bytes(face.TransformChain)
	if err != nil {
		return false
	}
	b, err := canonical.Bytes(source.TransformChain)
	if err != nil || !bytes.Equal(a, b) {
		return false
	}
	for _, p := range face.Points {
		if !unitLandmark(p.X) || !unitLandmark(p.Y) || !unitLandmark(p.Confidence) {
			return false
		}
	}
	return true
}

// ParseFace68 validates geometry and complete point fields; caller proves hash,
// catalog authorization and producer/model qualification separately.
func ParseFace68(raw []byte, source DefectSource) (Face68, error) {
	var face Face68
	if decodeExact(raw, &face) != nil || !faceMatchesSource(face, source) {
		return Face68{}, errors.New("invalid source-bound Face68")
	}
	return face, nil
}

// ResolveFaceLandmarks selects one unique object from an exact-source fan2d
// collection, validating every face in the document. Stored model/backend fields
// are content claims and do not certify a qualified producer.
func ResolveFaceLandmarks(raw []byte, source DefectSource, objectID uuid.UUID) (Face68, error) {
	doc, err := parseFaceLandmarks(raw, source)
	if err != nil {
		return Face68{}, err
	}
	if objectID != uuid.Nil {
		for _, face := range doc.Landmarks {
			if face.ObjectID == objectID {
				return face, nil
			}
		}
	}
	return Face68{}, errors.New("missing face object")
}

// ParseFaceLandmarkCollection validates the complete original fan2d collection.
// Returned faces own decoded points/chain bytes; model claims are not qualification.
func ParseFaceLandmarkCollection(raw []byte, source DefectSource) ([]Face68, error) {
	if len(raw) < 2 || len(raw) > 1048576 {
		return nil, errors.New("invalid FaceLandmarks size")
	}
	doc, err := parseFaceLandmarks(raw, source)
	if err != nil {
		return nil, err
	}
	return doc.Landmarks, nil
}

func parseFaceLandmarks(raw []byte, source DefectSource) (faceLandmarksDocument, error) {
	fail := func() (faceLandmarksDocument, error) {
		return faceLandmarksDocument{}, errors.New("invalid source-bound FaceLandmarks")
	}
	var doc faceLandmarksDocument
	if decodeExact(raw, &doc) != nil || !source.Image.valid() || doc.Image != source.Image || !doc.Faces.valid() || doc.ModelID != "fan2d" || doc.PreprocessingID != "fan2d_crop256_v1" || doc.Landmarks == nil || len(doc.Landmarks) > 100 || !unitLandmark(doc.MeanConfidence) || doc.Warnings == nil || len(doc.Warnings) > 3 {
		return fail()
	}
	if _, err := domain.ParseDigest(doc.ModelLockID); err != nil {
		return fail()
	}
	if doc.Backend != "cpu" && doc.Backend != "cuda" && doc.Backend != "mps" {
		return fail()
	}
	seen := map[uuid.UUID]bool{}
	confidenceSum, correction := 0.0, 0.0
	pointCount := 0
	for _, face := range doc.Landmarks {
		if seen[face.ObjectID] || !faceMatchesSource(face, source) {
			return fail()
		}
		seen[face.ObjectID] = true
		for _, point := range face.Points {
			// Compensated summation limits drift against NumPy's float64 mean.
			adjusted := point.Confidence - correction
			next := confidenceSum + adjusted
			correction = (next - confidenceSum) - adjusted
			confidenceSum = next
			pointCount++
		}

	}
	warnings := map[string]bool{}
	for _, warning := range doc.Warnings {
		if warnings[warning] {
			return fail()
		}
		warnings[warning] = true
		switch warning {
		case "no_faces", "low_confidence", "source_srgb_clipped":
		default:
			return fail()
		}
	}
	mean := 0.0
	if pointCount > 0 {
		mean = confidenceSum / float64(pointCount)
	}
	if math.Abs(mean-doc.MeanConfidence) > 1e-12 || warnings["no_faces"] != (len(doc.Landmarks) == 0) || warnings["low_confidence"] != (len(doc.Landmarks) > 0 && doc.MeanConfidence < .75) {
		return fail()
	}
	return doc, nil
}
