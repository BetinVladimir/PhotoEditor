package v2optical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type NegativeCalibration struct {
	SchemaVersion int           `json:"schema_version"`
	InputICCSHA   string        `json:"input_icc_sha"`
	DensityMatrix [3][3]float64 `json:"density_matrix"`
	DensityOffset [3]float64    `json:"density_offset"`
	DensityRange  [3]float64    `json:"density_range"`
	Qualification v2input.Ref   `json:"qualification"`
}

func (c NegativeCalibration) Compile() ([]byte, error) {
	if c.SchemaVersion != 1 || c.Qualification.ArtifactID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	for _, sha := range []string{c.InputICCSHA, c.Qualification.SHA256} {
		if _, err := domain.ParseDigest(sha); err != nil {
			return nil, v2input.ErrInvalid
		}
	}
	for _, row := range c.DensityMatrix {
		for _, n := range row {
			if !negativeRange(n, -10, 10) {
				return nil, v2input.ErrInvalid
			}
		}
	}
	for _, n := range c.DensityOffset {
		if !negativeRange(n, -10, 10) {
			return nil, v2input.ErrInvalid
		}
	}
	for _, n := range c.DensityRange {
		if !negativeRange(n, .001, 10) {
			return nil, v2input.ErrInvalid
		}
	}
	return canonical.Marshal(c)
}

// VerifyNegativeCalibration proves original bytes/schema/ICC binding only.
// Report content, importer authority and current policy remain separate gates.
func VerifyNegativeCalibration(params NegativeParams, raw []byte, inputICCSHA string) (NegativeCalibration, error) {
	invalid := func() (NegativeCalibration, error) { return NegativeCalibration{}, v2input.ErrInvalid }
	if _, _, err := params.Compile(); err != nil {
		return invalid()
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return invalid()
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != params.Calibration.SHA256 {
		return invalid()
	}
	var c NegativeCalibration
	if json.Unmarshal(raw, &c) != nil {
		return invalid()
	}
	body, err := c.Compile()
	if err != nil || !bytes.Equal(body, raw) || c.InputICCSHA != inputICCSHA {
		return invalid()
	}
	return c, nil
}
