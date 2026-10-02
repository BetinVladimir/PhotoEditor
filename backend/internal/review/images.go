package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"math"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

type ColorSpace struct {
	ProfileID string `json:"profile_id"`
	ICCSHA256 string `json:"icc_sha256"`
	Encoding  string `json:"encoding"`
}
type WorkingImage struct {
	Pixels         ArtifactRef     `json:"pixels"`
	Canvas         Canvas          `json:"canvas"`
	Color          ColorSpace      `json:"color"`
	DType          string          `json:"dtype"`
	Alpha          *ArtifactRef    `json:"alpha"`
	TransformChain json.RawMessage `json:"transform_chain"`
}
type MaskRef struct {
	Artifact       ArtifactRef     `json:"artifact"`
	Canvas         Canvas          `json:"canvas"`
	TransformChain json.RawMessage `json:"transform_chain"`
	ObjectID       *uuid.UUID      `json:"object_id"`
	Selector       string          `json:"selector"`
	Confidence     float64         `json:"confidence"`
	ModelLockID    *string         `json:"model_lock_id"`
	Protected      bool            `json:"protected"`
}
type transformStep struct {
	Kind          string       `json:"kind"`
	From          Canvas       `json:"from_canvas"`
	To            Canvas       `json:"to_canvas"`
	Invertibility string       `json:"invertibility"`
	ValidRegion   rectangle    `json:"valid_region"`
	Matrix        *[]float64   `json:"forward_matrix,omitempty"`
	InverseField  *ArtifactRef `json:"inverse_field,omitempty"`
	Crop          *rectangle   `json:"crop_rect,omitempty"`
	PixelCenters  *string      `json:"pixel_centers,omitempty"`
}
type transformChain struct {
	Steps []transformStep `json:"steps"`
	Hash  string          `json:"hash"`
}

func decodeExact(raw []byte, out any) error {
	if len(raw) > 1048576 {
		return errors.New("contract exceeds byte limit")
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(out); err != nil {
		return err
	}
	roundtrip, err := canonical.Marshal(out)
	if err != nil {
		return err
	}
	if !bytes.Equal(roundtrip, normalized) {
		return errors.New("missing or null contract fields")
	}
	return nil
}
func validCanvas(c Canvas) bool {
	_, err := domain.ParseDigest(c.SourceSHA256)
	return err == nil && c.Width >= 1 && c.Height >= 1 && c.Width <= 2147483647 && c.Height <= 2147483647 && c.Width*c.Height <= 9007199254740991 && c.Orientation == 1 && spaceIDPattern.MatchString(c.SpaceID)
}
func rectWithin(r rectangle, c Canvas) bool {
	return r.X >= 0 && r.Y >= 0 && r.X < c.Width && r.Y < c.Height && r.Width >= 1 && r.Height >= 1 && r.Width <= c.Width-r.X && r.Height <= c.Height-r.Y
}

func validateTransformChain(raw []byte, target Canvas) error {
	invalid := errors.New("invalid transform chain")
	var chain transformChain
	if err := decodeExact(raw, &chain); err != nil {
		return err
	}
	if chain.Steps == nil || len(chain.Steps) > 128 {
		return invalid
	}
	digest, err := canonical.Hash(struct {
		Steps []transformStep `json:"steps"`
	}{chain.Steps})
	if err != nil || digest != chain.Hash {
		return invalid
	}
	for i, step := range chain.Steps {
		if !validCanvas(step.From) || !validCanvas(step.To) || step.From.SourceSHA256 != step.To.SourceSHA256 || !rectWithin(step.ValidRegion, step.To) ||
			(step.Invertibility != "invertible" && step.Invertibility != "partial" && step.Invertibility != "noninvertible") || (i > 0 && chain.Steps[i-1].To != step.From) {
			return invalid
		}
		switch step.Kind {
		case "matrix":
			if step.Matrix == nil || step.InverseField != nil || step.Crop != nil || step.PixelCenters != nil || len(*step.Matrix) != 9 {
				return invalid
			}
			m := *step.Matrix
			for _, v := range m {
				if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e12 {
					return invalid
				}
			}
			a, b, c, d, e, f, g, h, k := m[0], m[1], m[2], m[3], m[4], m[5], m[6], m[7], m[8]
			determinant := a*(e*k-f*h) - b*(d*k-f*g) + c*(d*h-e*g)
			scale := math.Max(math.Max(math.Max(math.Abs(a), math.Abs(b)), math.Max(math.Abs(d), math.Abs(e))), math.Max(math.Max(math.Abs(g)*float64(step.From.Width), math.Abs(h)*float64(step.From.Height)), math.Abs(k)))
			if scale == 0 || math.Abs(determinant)/math.Pow(scale, 3) < 1e-12 {
				return invalid
			}
			low, high := math.Inf(1), math.Inf(-1)
			for _, x := range []float64{0, float64(step.From.Width - 1)} {
				for _, y := range []float64{0, float64(step.From.Height - 1)} {
					v := g*x + h*y + k
					low = math.Min(low, v)
					high = math.Max(high, v)
				}
			}
			if low <= 0 && high >= 0 {
				return invalid
			}
		case "dense_warp":
			if step.InverseField == nil || !step.InverseField.valid() || step.Matrix != nil || step.Crop != nil || (step.PixelCenters != nil && *step.PixelCenters != "half_pixel") {
				return invalid
			}
		case "crop":
			if step.Crop == nil || step.Matrix != nil || step.InverseField != nil || step.PixelCenters != nil || !rectWithin(*step.Crop, step.From) || step.Crop.Width != step.To.Width || step.Crop.Height != step.To.Height {
				return invalid
			}
		case "resize":
			if step.PixelCenters == nil || *step.PixelCenters != "half_pixel" || step.Matrix != nil || step.InverseField != nil || step.Crop != nil {
				return invalid
			}
		default:
			return invalid
		}
	}
	if len(chain.Steps) > 0 && chain.Steps[len(chain.Steps)-1].To != target {
		return invalid
	}
	return nil
}

// ParseWorkingImage validates metadata, never decodes or allocates pixel arrays.
// The caller verifies artifact SHA/provenance before passing these bytes.
func ParseWorkingImage(raw []byte) (WorkingImage, error) {
	var image WorkingImage
	if err := decodeExact(raw, &image); err != nil {
		return WorkingImage{}, err
	}
	if !image.Pixels.valid() || !validCanvas(image.Canvas) || image.Canvas.Width*image.Canvas.Height > 200000000 || (image.DType != "float32" && image.DType != "uint16") || (image.Alpha != nil && !image.Alpha.valid()) {
		return WorkingImage{}, errors.New("invalid working image")
	}
	if _, err := domain.ParseDigest(image.Color.ICCSHA256); err != nil {
		return WorkingImage{}, err
	}
	if !(image.Color.ProfileID == "work_rec2020_gamma24_v1" && image.Color.Encoding == "gamma24") && !(image.Color.ProfileID == "linear_srgb_v1" && image.Color.Encoding == "linear") {
		return WorkingImage{}, errors.New("invalid working color space")
	}
	if err := validateTransformChain(image.TransformChain, image.Canvas); err != nil {
		return WorkingImage{}, err
	}
	return image, nil
}
func ParseMaskRef(raw []byte) (MaskRef, error) {
	var mask MaskRef
	if err := decodeExact(raw, &mask); err != nil {
		return MaskRef{}, err
	}
	if !mask.Artifact.valid() || !validCanvas(mask.Canvas) || mask.Selector == "" || len(mask.Selector) > 120 || math.IsNaN(mask.Confidence) || math.IsInf(mask.Confidence, 0) || mask.Confidence < 0 || mask.Confidence > 1 {
		return MaskRef{}, errors.New("invalid mask ref")
	}
	if mask.ModelLockID != nil {
		if _, err := domain.ParseDigest(*mask.ModelLockID); err != nil {
			return MaskRef{}, err
		}
	}
	if err := validateTransformChain(mask.TransformChain, mask.Canvas); err != nil {
		return MaskRef{}, err
	}
	return mask, nil
}

// LastTransformStep returns the exact validated final step for downstream
// mask.transform parameters. It never trusts an unvalidated chain fragment.
func LastTransformStep(image WorkingImage) (json.RawMessage, error) {
	if err := validateTransformChain(image.TransformChain, image.Canvas); err != nil {
		return nil, err
	}
	var chain struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(image.TransformChain, &chain); err != nil || len(chain.Steps) == 0 {
		return nil, errors.New("working image has no transform step")
	}
	return canonical.Bytes(chain.Steps[len(chain.Steps)-1])
}

func ParseTransformedMask(raw []byte) (MaskRef, error) {
	var value struct {
		Params       json.RawMessage `json:"params"`
		Mask         MaskRef         `json:"mask"`
		PNG          ArtifactRef     `json:"png"`
		DType        string          `json:"dtype"`
		GuidedRadius *int            `json:"guided_radius"`
		Algorithm    string          `json:"algorithm"`
	}
	if err := decodeExact(raw, &value); err != nil {
		return MaskRef{}, err
	}
	if value.Algorithm != "mask_transform_v1" || value.DType != "float32" || !value.PNG.valid() {
		return MaskRef{}, errors.New("invalid transformed mask")
	}
	encoded, err := canonical.Marshal(value.Mask)
	if err != nil {
		return MaskRef{}, err
	}
	return ParseMaskRef(encoded)
}
func (i WorkingImage) Source(ref ArtifactRef) DefectSource {
	return DefectSource{Image: ref, Pixels: i.Pixels, Canvas: i.Canvas, TransformChain: append(json.RawMessage(nil), i.TransformChain...)}
}
