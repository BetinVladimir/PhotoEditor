package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/v2input"
)

// ValidatePlanarHomography mirrors the worker's 33x33 source-domain safety gate.
// It proves neither planar scene evidence nor transferred mask equivalence.
func ValidatePlanarHomography(matrix []float64, width, height int64) bool {
	if len(matrix) != 9 || width < 8 || height < 8 || width > 200000000 || height > 200000000/width {
		return false
	}
	var m [9]float64
	for i, v := range matrix {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
		m[i] = v
	}
	if math.Abs(m[8]) < 1e-12 {
		return false
	}
	scale := m[8]
	for i := range m {
		m[i] /= scale
		if math.IsNaN(m[i]) || math.IsInf(m[i], 0) {
			return false
		}
	}
	det := m[0]*(m[4]*m[8]-m[5]*m[7]) - m[1]*(m[3]*m[8]-m[5]*m[6]) + m[2]*(m[3]*m[7]-m[4]*m[6])
	if math.IsNaN(det) || math.IsInf(det, 0) || math.Abs(det) < 1e-12 {
		return false
	}
	for y := 0; y <= 32; y++ {
		for x := 0; x <= 32; x++ {
			denominator := m[6]*float64(x)*float64(width-1)/32 + m[7]*float64(y)*float64(height-1)/32 + m[8]
			if denominator <= 1e-8 || !(det/(denominator*denominator*denominator) > 0) {
				return false
			}
		}
	}
	return true
}

// ValidateApprovedExposureGeometry binds exact original review/candidate and
// source/mask/transform bytes before a downstream task can be admitted. Object
// authorization, ICC and actual mask/protection arrays remain separate checks.
func ValidateApprovedExposureGeometry(prepared PreparedReview, targets []LockedTarget, request ApplyDecision, targetID uuid.UUID,
	source, reference, mask, transform []byte) error {
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil {
		return err
	}
	var edit *ApprovedEdit
	for i := range edits {
		if edits[i].TargetID == targetID {
			edit = &edits[i]
		}
	}
	if edit == nil || edit.Kind != "local_exposure" {
		return ErrInvalid
	}
	owned, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA)
	if err != nil {
		return err
	}
	_, err = v2input.ResolveSemanticSourceMetadata(edit.Source, source)
	if err != nil {
		return ErrInvalid
	}
	referenceMetadata, err := v2input.ResolveSemanticSourceMetadata(owned.Intent.Reference, reference)
	if err != nil {
		return ErrInvalid
	}
	if _, err := v2input.ResolveSemanticMaskPixels(edit.Mask, mask, edit.Source, source, false); err != nil {
		return ErrInvalid
	}
	if len(transform) < 2 || len(transform) > 1048576 {
		return ErrInvalid
	}
	sum := sha256.Sum256(transform)
	if hex.EncodeToString(sum[:]) != edit.Transform.SHA256 {
		return ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(transform, &fields) != nil || len(fields) != 5 {
		return ErrInvalid
	}
	for _, key := range []string{"schema_version", "from_canvas_hash", "to_canvas_hash", "matrix", "kind"} {
		if fields[key] == nil {
			return ErrInvalid
		}
	}
	var resolved struct {
		Schema int       `json:"schema_version"`
		From   string    `json:"from_canvas_hash"`
		To     string    `json:"to_canvas_hash"`
		Matrix []float64 `json:"matrix"`
		Kind   string    `json:"kind"`
	}
	if decodeStrict(transform, &resolved) != nil || resolved.Schema != 1 || resolved.Kind != "homography" || resolved.From != owned.Intent.Reference.CanvasHash || resolved.To != edit.Source.CanvasHash || !ValidatePlanarHomography(resolved.Matrix, referenceMetadata.Width, referenceMetadata.Height) {
		return ErrInvalid
	}
	for _, value := range resolved.Matrix {
		if value < -1e8 || value > 1e8 {
			return ErrInvalid
		}
	}
	// ResolveSemanticMaskPixels proves exact source canvas/chain; retain the
	// candidate's confidence and scientific lock instead of accepting new metadata.
	var maskFields struct {
		Confidence float64 `json:"confidence"`
		Selector   string  `json:"selector"`
		ModelLock  *string `json:"model_lock_id"`
	}
	if json.Unmarshal(mask, &maskFields) != nil || maskFields.Selector != "manual" || maskFields.ModelLock == nil || *maskFields.ModelLock != prepared.modelSHA {
		return ErrInvalid
	}
	for _, document := range owned.documents {
		if document.Source.Ref == edit.Candidate {
			candidate, err := v2input.DecodeSemanticCandidate(document.Body)
			if err != nil || candidate.Confidence != maskFields.Confidence {
				return ErrInvalid
			}
			return nil
		}
	}
	return ErrInvalid
}
