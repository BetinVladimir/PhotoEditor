package v2optical

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type PerspectivePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type PerspectiveParams struct {
	SchemaVersion   int                           `json:"schema_version"`
	Base            v2input.SessionVersionBinding `json:"base"`
	Strength        float64                       `json:"strength"`
	Mode            string                        `json:"mode"`
	Corners         []PerspectivePoint            `json:"corners"`
	Lines           *v2input.Ref                  `json:"lines"`
	MaxCropFraction float64                       `json:"max_crop_fraction"`
}

func validPerspectiveRef(ref v2input.Ref) bool {
	if ref.ArtifactID == uuid.Nil {
		return false
	}
	_, err := domain.ParseDigest(ref.SHA256)
	return err == nil
}
func (p PerspectiveParams) Compile() ([]byte, string, error) {
	invalid := func() ([]byte, string, error) { return nil, "", v2input.ErrInvalid }
	if p.SchemaVersion != 1 || p.Base.AssetVersionID == uuid.Nil || !validPerspectiveRef(p.Base.Source) ||
		!negativeRange(p.Strength, 0, 1) || !negativeRange(p.MaxCropFraction, 0, 1) {
		return invalid()
	}
	if _, err := domain.ParseDigest(p.Base.CanvasHash); err != nil {
		return invalid()
	}
	switch p.Mode {
	case "manual":
		if len(p.Corners) != 4 || p.Lines != nil {
			return invalid()
		}
		for _, point := range p.Corners {
			if !negativeRange(point.X, 0, 1) || !negativeRange(point.Y, 0, 1) {
				return invalid()
			}
		}
		for i := 0; i < 4; i++ {
			a, b, c := p.Corners[i], p.Corners[(i+1)%4], p.Corners[(i+2)%4]
			if (b.X-a.X)*(c.Y-b.Y)-(b.Y-a.Y)*(c.X-b.X) <= 0 {
				return invalid()
			}
		}
	case "auto":
		if p.Corners != nil || p.Lines == nil || !validPerspectiveRef(*p.Lines) {
			return invalid()
		}
	default:
		return invalid()
	}
	body, err := canonical.Marshal(p)
	if err != nil {
		return invalid()
	}
	digest, err := canonical.Hash(p)
	if err != nil {
		return invalid()
	}
	return body, digest, nil
}
func ParsePerspectiveParams(raw []byte) (PerspectiveParams, error) {
	var p PerspectiveParams
	if len(raw) < 2 || len(raw) > 65536 || json.Unmarshal(raw, &p) != nil {
		return PerspectiveParams{}, v2input.ErrInvalid
	}
	body, _, err := p.Compile()
	if err != nil || !bytes.Equal(body, raw) {
		return PerspectiveParams{}, v2input.ErrInvalid
	}
	return p, nil
}

type PerspectiveLine struct {
	A      PerspectivePoint `json:"a"`
	B      PerspectivePoint `json:"b"`
	Family string           `json:"family"`
}
type PerspectiveLineSegments struct {
	SchemaVersion int               `json:"schema_version"`
	Lines         []PerspectiveLine `json:"lines"`
}

func (p PerspectiveLineSegments) Compile() ([]byte, string, error) {
	invalid := func() ([]byte, string, error) { return nil, "", v2input.ErrInvalid }
	if p.SchemaVersion != 1 || len(p.Lines) < 4 || len(p.Lines) > 1000 {
		return invalid()
	}
	counts := map[string]int{}
	for _, line := range p.Lines {
		if line.Family != "horizontal" && line.Family != "vertical" {
			return invalid()
		}
		if line.A == line.B {
			return invalid()
		}
		for _, point := range []PerspectivePoint{line.A, line.B} {
			if !negativeRange(point.X, 0, 1) || !negativeRange(point.Y, 0, 1) {
				return invalid()
			}
		}
		counts[line.Family]++
	}
	if counts["horizontal"] < 2 || counts["vertical"] < 2 {
		return invalid()
	}
	body, err := canonical.Marshal(p)
	if err != nil {
		return invalid()
	}
	digest, err := canonical.Hash(p)
	if err != nil {
		return invalid()
	}
	return body, digest, nil
}
func ParsePerspectiveLineSegments(raw []byte) (PerspectiveLineSegments, error) {
	var p PerspectiveLineSegments
	if len(raw) < 2 || len(raw) > 1<<20 || json.Unmarshal(raw, &p) != nil {
		return PerspectiveLineSegments{}, v2input.ErrInvalid
	}
	body, _, err := p.Compile()
	if err != nil || !bytes.Equal(body, raw) {
		return PerspectiveLineSegments{}, v2input.ErrInvalid
	}
	return p, nil
}
