package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"math"
)

type manualPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type manualShape struct {
	Kind     string        `json:"kind"`
	Sequence int64         `json:"sequence"`
	Mode     string        `json:"mode"`
	Points   []manualPoint `json:"points"`
	Radius   *float64      `json:"radius,omitempty"`
	Hardness *float64      `json:"hardness,omitempty"`
}
type manualVectors struct {
	Version   string        `json:"version"`
	SourceSHA string        `json:"source_sha256"`
	Canvas    Canvas        `json:"canvas"`
	Shapes    []manualShape `json:"shapes"`
}
type manualRasterParams struct {
	Image    ArtifactRef    `json:"image"`
	Vectors  *ArtifactRef   `json:"vectors"`
	Inline   *manualVectors `json:"inline_vectors"`
	ObjectID *uuid.UUID     `json:"object_id"`
}

// VerifyManualMaskParams binds original producer Params to its verified root.
// Returns the external vector ref when present; caller must read and authorize
// that original dependency separately. Inline vector geometry is checked here.
// This does not rerun rasterization or certify resulting pixel coverage.
func VerifyManualMaskParams(raw []byte, ref ArtifactRef, rootRaw []byte, rootRef ArtifactRef, expected ProtectionMask, source DefectSource) (*ArtifactRef, error) {
	fail := func() (*ArtifactRef, error) { return nil, errors.New("invalid manual mask Params") }
	if !ref.valid() || len(raw) < 2 || len(raw) > 1048576 || expected.Selector != "manual" {
		return fail()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != ref.SHA256 || VerifyFaceMaskRoot(rootRaw, rootRef, expected, source) != nil {
		return fail()
	}
	var chain transformChain
	if decodeExact(source.TransformChain, &chain) != nil || len(chain.Steps) != 0 {
		return fail()
	}
	var params manualRasterParams
	var root rasterizedFaceMask
	if decodeExact(raw, &params) != nil || decodeExact(rootRaw, &root) != nil || params.Image != source.Image || (params.Vectors == nil) == (params.Inline == nil) {
		return fail()
	}
	if (params.ObjectID == nil) != (expected.ObjectID == nil) || (params.ObjectID != nil && (*params.ObjectID == uuid.Nil || *params.ObjectID != *expected.ObjectID)) {
		return fail()
	}
	if params.Vectors != nil {
		if !params.Vectors.valid() || root.Vectors != *params.Vectors {
			return fail()
		}
		owned := *params.Vectors
		return &owned, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || !manualGeometryFieldsPresent(fields["inline_vectors"]) {
		return fail()
	}
	if root.Vectors != ref || !validManualVectors(*params.Inline, source) {
		return fail()
	}
	return nil, nil
}

func validManualVectors(v manualVectors, source DefectSource) bool {
	if v.Version != "manual_mask_v1" || v.SourceSHA != source.Canvas.SourceSHA256 || v.Canvas != source.Canvas || v.Shapes == nil || len(v.Shapes) > 1024 || int64(v.Canvas.Width)*int64(v.Canvas.Height) > 200000000 {
		return false
	}
	seen := map[int64]bool{}
	points := 0
	unit := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }
	for _, shape := range v.Shapes {
		if shape.Sequence < 0 || shape.Sequence > 2147483647 || seen[shape.Sequence] || (shape.Mode != "add" && shape.Mode != "subtract" && shape.Mode != "protect") || len(shape.Points) > 4096 {
			return false
		}
		seen[shape.Sequence] = true
		points += len(shape.Points)
		if points > 10000 {
			return false
		}
		switch shape.Kind {
		case "stroke":
			if len(shape.Points) < 1 || shape.Radius == nil || shape.Hardness == nil || !unit(*shape.Radius) || *shape.Radius == 0 || !unit(*shape.Hardness) {
				return false
			}
		case "polygon":
			if len(shape.Points) < 3 || shape.Radius != nil || shape.Hardness != nil {
				return false
			}
		default:
			return false
		}
		for _, p := range shape.Points {
			if !unit(p.X) || !unit(p.Y) {
				return false
			}
		}
	}
	return true
}

// VerifyManualVectorsDocument validates an external original vector document.
// Storage ownership and declaration in producer inputs are caller obligations.
func VerifyManualVectorsDocument(raw []byte, ref ArtifactRef, source DefectSource) error {
	fail := func() error { return errors.New("invalid manual vector document") }
	if !ref.valid() || len(raw) < 2 || len(raw) > 1048576 || !validCanvas(source.Canvas) {
		return fail()
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return fail()
	}
	var vectors manualVectors
	var chain transformChain
	if validateTransformChain(source.TransformChain, source.Canvas) != nil || decodeExact(source.TransformChain, &chain) != nil || len(chain.Steps) != 0 || decodeExact(raw, &vectors) != nil || !manualGeometryFieldsPresent(raw) || !validManualVectors(vectors, source) {
		return fail()
	}
	return nil
}

// Numeric zero is valid geometry, but a missing/null field is not zero.
// Typed geometry checks alone cannot distinguish those JSON representations.
func manualGeometryFieldsPresent(raw []byte) bool {
	var doc struct {
		Shapes []struct {
			Sequence *int64 `json:"sequence"`
			Points   []struct {
				X *float64 `json:"x"`
				Y *float64 `json:"y"`
			} `json:"points"`
		} `json:"shapes"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Shapes == nil {
		return false
	}
	for _, shape := range doc.Shapes {
		if shape.Sequence == nil {
			return false
		}
		for _, point := range shape.Points {
			if point.X == nil || point.Y == nil {
				return false
			}
		}
	}
	return true
}

// ParseManualVectorsCanvas validates a standalone ManualMask structurally.
// Returned canvas is declared metadata, not source/tenant authority. Callers
// must still bind original bytes and compare the independently read source.
func ParseManualVectorsCanvas(raw []byte) (Canvas, error) {
	var zero Canvas
	if len(raw) < 2 || len(raw) > 1048576 {
		return zero, errors.New("invalid manual vector document")
	}
	var vectors manualVectors
	if decodeExact(raw, &vectors) != nil || !validCanvas(vectors.Canvas) || !manualGeometryFieldsPresent(raw) || !validManualVectors(vectors, DefectSource{Canvas: vectors.Canvas}) {
		return zero, errors.New("invalid manual vector document")
	}
	return vectors.Canvas, nil
}
