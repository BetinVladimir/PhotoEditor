package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
)

type NormalizedRect struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}
type FaceBox struct {
	ObjectID   uuid.UUID      `json:"object_id"`
	BBox       NormalizedRect `json:"bbox_normalized"`
	Confidence float64        `json:"confidence"`
}
type FaceBoxes struct {
	Image                 ArtifactRef `json:"image"`
	Faces                 []FaceBox   `json:"faces"`
	ModelID               string      `json:"model_id"`
	ModelLockID           string      `json:"model_lock_id"`
	Backend               string      `json:"backend"`
	PreprocessingID       string      `json:"preprocessing_id"`
	PreviewWidth          int         `json:"preview_width"`
	PreviewHeight         int         `json:"preview_height"`
	CandidateCount        int         `json:"candidate_count"`
	AcceptedCount         int         `json:"accepted_count"`
	AlphaExcludedFraction float64     `json:"alpha_excluded_fraction"`
	Warnings              []string    `json:"warnings"`
}

// ParseFaceBoxes validates exact-source detector content. Caller separately
// proves original hash, canonical producer, Params and qualified model/runtime.
func ParseFaceBoxes(raw []byte, source DefectSource) (FaceBoxes, error) {
	fail := func() (FaceBoxes, error) { return FaceBoxes{}, errors.New("invalid source-bound FaceBoxes") }
	var doc FaceBoxes
	if decodeExact(raw, &doc) != nil || !source.Image.valid() || doc.Image != source.Image || doc.ModelID != "sfd" || doc.PreprocessingID != "sfd_long1024_bgr_v1" || doc.Faces == nil || len(doc.Faces) > 100 || doc.PreviewWidth < 1 || doc.PreviewWidth > 1024 || doc.PreviewHeight < 1 || doc.PreviewHeight > 1024 || doc.CandidateCount < len(doc.Faces) || doc.CandidateCount > 100000 || doc.AcceptedCount != len(doc.Faces) || !unitLandmark(doc.AlphaExcludedFraction) || doc.Warnings == nil || len(doc.Warnings) > 3 {
		return fail()
	}
	if _, err := domain.ParseDigest(doc.ModelLockID); err != nil {
		return fail()
	}
	if doc.Backend != "cpu" && doc.Backend != "cuda" && doc.Backend != "mps" {
		return fail()
	}
	seen := map[uuid.UUID]bool{}
	for _, face := range doc.Faces {
		r := face.BBox
		if face.ObjectID == uuid.Nil || seen[face.ObjectID] || !unitLandmark(face.Confidence) || !unitLandmark(r.X0) || !unitLandmark(r.Y0) || !unitLandmark(r.X1) || !unitLandmark(r.Y1) || r.X1 <= r.X0 || r.Y1 <= r.Y0 {
			return fail()
		}
		seen[face.ObjectID] = true
	}
	warnings := map[string]bool{}
	for _, warning := range doc.Warnings {
		if warnings[warning] {
			return fail()
		}
		warnings[warning] = true
		switch warning {
		case "no_faces", "faces_truncated", "source_srgb_clipped":
		default:
			return fail()
		}
	}
	if warnings["no_faces"] != (len(doc.Faces) == 0) {
		return fail()
	}
	return doc, nil
}

// VerifyLandmarkDetection verifies the original detection ref/hash declared by
// a source-bound landmark collection and requires exactly the detected objects.
// Producer provenance and scientific qualification are independent gates.
func VerifyLandmarkDetection(landmarkRaw []byte, detection ArtifactRef, detectionRaw []byte, source DefectSource) error {
	doc, err := parseFaceLandmarks(landmarkRaw, source)
	if err != nil {
		return err
	}
	if !detection.valid() || doc.Faces != detection || len(detectionRaw) < 2 || len(detectionRaw) > 1048576 {
		return errors.New("invalid landmark detection binding")
	}
	digest := sha256.Sum256(detectionRaw)
	if hex.EncodeToString(digest[:]) != detection.SHA256 {
		return errors.New("detection SHA mismatch")
	}
	boxes, err := ParseFaceBoxes(detectionRaw, source)
	if err != nil {
		return err
	}
	if len(boxes.Faces) != len(doc.Landmarks) {
		return errors.New("landmark detection cardinality mismatch")
	}
	objects := map[uuid.UUID]bool{}
	for _, box := range boxes.Faces {
		objects[box.ObjectID] = true
	}
	for _, face := range doc.Landmarks {
		if !objects[face.ObjectID] {
			return errors.New("undetected landmark object")
		}
	}
	return nil
}
