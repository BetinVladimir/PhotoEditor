package v2portrait

import (
	"photoeditor/backend/internal/v2input"
)

type RelightLight struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Radius float64 `json:"radius"`
	EV     float64 `json:"ev"`
}

// RelightParams describes authored controls only. Compilation grants no
// qualification, producer authority or permission to execute a task.
type RelightParams struct {
	SchemaVersion int                           `json:"schema_version"`
	Base          v2input.SessionVersionBinding `json:"base"`
	Strength      float64                       `json:"strength"`
	Depth         v2input.Ref                   `json:"depth"`
	Subject       v2input.Ref                   `json:"subject"`
	Protected     v2input.Ref                   `json:"protected"`
	Lights        []RelightLight                `json:"lights"`
	Calibration   v2input.Ref                   `json:"calibration"`
}

func (p RelightParams) Compile() ([]byte, string, error) {
	if !validLocalBase(p.SchemaVersion, p.Base, p.Subject, p.Protected, p.Strength) ||
		!validLocalBase(p.SchemaVersion, p.Base, p.Depth, p.Calibration, p.Strength) ||
		len(p.Lights) < 1 || len(p.Lights) > 4 {
		return nil, "", v2input.ErrInvalid
	}
	for _, light := range p.Lights {
		if !finiteRange(light.X, -1, 2) || !finiteRange(light.Y, -1, 2) ||
			!finiteRange(light.Radius, .05, 2) || !finiteRange(light.EV, -1, 1) {
			return nil, "", v2input.ErrInvalid
		}
	}
	return compileParams(p)
}
