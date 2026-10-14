package v2render

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
)

type MultiImageReport struct {
	SchemaVersion       int                             `json:"schema_version"`
	Status              string                          `json:"status"`
	Operation           string                          `json:"operation"`
	SkippedCount        int                             `json:"skipped_count"`
	Reason              string                          `json:"reason"`
	OutsideMaskMaxError float64                         `json:"outside_mask_max_error"`
	ClippingFraction    float64                         `json:"clipping_fraction"`
	Identity            *v2input.Ref                    `json:"identity"`
	Transform           *v2input.Ref                    `json:"transform"`
	Sources             []v2input.SessionVersionBinding `json:"sources"`
	Registrations       []v2input.Ref                   `json:"registrations"`
}

// ParseMultiImageReport retains measured producer reasons and all ordered
// sources. It is a contract check, not an independent quality assessment.
func ParseMultiImageReport(raw []byte, binding AdoptedMultiBinding, parent v2input.Ref) (MultiImageReport, error) {
	var report MultiImageReport
	if len(raw) < 2 || len(raw) > 65536 {
		return report, v2input.ErrInvalid
	}
	if _, err := v2draft.DocumentChildren(raw, "ImageReport", parent); err != nil {
		return report, err
	}
	if json.Unmarshal(raw, &report) != nil {
		return MultiImageReport{}, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(report)
	if err != nil || !bytes.Equal(exact, raw) {
		return MultiImageReport{}, v2input.ErrInvalid
	}
	frames, _, calibration, err := binding.Inputs()
	if err != nil {
		return MultiImageReport{}, err
	}
	if report.SchemaVersion != 1 || report.Status != "review" || report.Operation != binding.Operation || report.SkippedCount != 0 || report.Identity != nil || report.Transform != nil || len(report.Sources) != len(frames) || len(report.Registrations) != len(frames) {
		return MultiImageReport{}, v2input.ErrInvalid
	}
	prefix := strings.ToUpper(strings.TrimPrefix(binding.Operation, "optical.")) + "_REVIEW;motion_fraction="
	if !strings.HasPrefix(report.Reason, prefix) {
		return MultiImageReport{}, v2input.ErrInvalid
	}
	reason := strings.Split(strings.TrimPrefix(report.Reason, prefix), ";")
	if len(reason) > 2 || len(reason) == 2 && reason[1] != "REGISTRATION_INCONSISTENT" {
		return MultiImageReport{}, v2input.ErrInvalid
	}
	fraction, err := strconv.ParseFloat(reason[0], 64)
	if err != nil || math.IsNaN(fraction) || math.IsInf(fraction, 0) || fraction < 0 || fraction > 1 {
		return MultiImageReport{}, v2input.ErrInvalid
	}
	used := map[uuid.UUID]bool{binding.Result.ArtifactID: true, parent.ArtifactID: true, calibration.ArtifactID: true}
	for i, frame := range frames {
		if report.Sources[i] != frame {
			return MultiImageReport{}, v2input.ErrInvalid
		}
		used[frame.Source.ArtifactID] = true
	}
	for _, ref := range report.Registrations {
		if !validLayerArtifactRef(ref) || used[ref.ArtifactID] {
			return MultiImageReport{}, v2input.ErrInvalid
		}
		used[ref.ArtifactID] = true
	}
	return report, nil
}

type MultiRegistration struct {
	SchemaVersion int                           `json:"schema_version"`
	Source        v2input.SessionVersionBinding `json:"source"`
	ToCanvasHash  string                        `json:"to_canvas_hash"`
	Homography    []float64                     `json:"homography"`
	InverseField  *v2input.Ref                  `json:"inverse_field"`
}

// The catalogue must still bind the projection field and homography to the
// actual output geometry. This parser does not replay registration or pixels.
func ParseMultiRegistration(raw []byte, source v2input.SessionVersionBinding, targetCanvas string, nonlinear bool) (MultiRegistration, error) {
	var value MultiRegistration
	invalid := func() (MultiRegistration, error) { return MultiRegistration{}, v2input.ErrInvalid }
	if len(raw) < 2 || len(raw) > 65536 || json.Unmarshal(raw, &value) != nil {
		return invalid()
	}
	exact, err := canonical.Marshal(value)
	if err != nil || !bytes.Equal(exact, raw) || value.SchemaVersion != 1 || value.Source != source || value.ToCanvasHash != targetCanvas || len(value.Homography) != 9 {
		return invalid()
	}
	if _, err := domain.ParseDigest(targetCanvas); err != nil {
		return invalid()
	}
	if source.AssetVersionID == uuid.Nil || !validLayerArtifactRef(source.Source) {
		return invalid()
	}
	if _, err := domain.ParseDigest(source.CanvasHash); err != nil {
		return invalid()
	}
	if nonlinear != (value.InverseField != nil) || value.InverseField != nil && (!validLayerArtifactRef(*value.InverseField) || value.InverseField.ArtifactID == source.Source.ArtifactID) {
		return invalid()
	}
	for _, x := range value.Homography {
		if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) > 1e8 {
			return invalid()
		}
	}
	h := value.Homography
	det := h[0]*(h[4]*h[8]-h[5]*h[7]) - h[1]*(h[3]*h[8]-h[5]*h[6]) + h[2]*(h[3]*h[7]-h[4]*h[6])
	if math.Abs(h[8]) <= 1e-10 || math.Abs(det) <= 1e-10 || math.IsNaN(det) || math.IsInf(det, 0) {
		return invalid()
	}
	return value, nil
}
