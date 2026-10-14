package workflow

import (
	"bytes"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// PerspectiveLinesExecutionPolicy is deployment configuration, never request
// Params. Its runtime digest must cover the detector's OpenCV dependency too.
// A policy pins execution; it does not attest photographic quality.
type PerspectiveLinesExecutionPolicy struct {
	body       json.RawMessage
	sha        string
	workingICC string
}

type perspectiveLinesExecutionPolicyBody struct {
	SchemaVersion    int    `json:"schema_version"`
	Operation        string `json:"operation"`
	OperationVersion int    `json:"operation_version"`
	Backend          string `json:"backend"`
	Precision        string `json:"precision"`
	RendererVersion  string `json:"renderer_version"`
	WorkingICC       string `json:"working_icc_sha"`
	RuntimeLockSHA   string `json:"runtime_lock_sha"`
	MaxPixels        int64  `json:"max_pixels"`
	ProxyEdge        int    `json:"proxy_edge"`
	MaxLines         int    `json:"max_lines"`
}

func CompilePerspectiveLinesExecutionPolicy(workingICC, detectorRuntimeLockSHA string) (PerspectiveLinesExecutionPolicy, error) {
	if domainDigestInvalid(workingICC) || domainDigestInvalid(detectorRuntimeLockSHA) {
		return PerspectiveLinesExecutionPolicy{}, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(perspectiveLinesExecutionPolicyBody{
		SchemaVersion: 1, Operation: "optical.perspective_lines", OperationVersion: 1,
		Backend: "cpu", Precision: "float32", RendererVersion: "v2.2",
		WorkingICC: workingICC, RuntimeLockSHA: detectorRuntimeLockSHA,
		MaxPixels: 200000000, ProxyEdge: 1600, MaxLines: 128,
	})
	if err != nil {
		return PerspectiveLinesExecutionPolicy{}, err
	}
	return PerspectiveLinesExecutionPolicy{body: raw, sha: renderDerivativeBytesSHA(raw), workingICC: workingICC}, nil
}

func (p PerspectiveLinesExecutionPolicy) Bytes() json.RawMessage { return bytes.Clone(p.body) }
func (p PerspectiveLinesExecutionPolicy) SHA256() string         { return p.sha }

// ForPlan refuses zero, mutated and foreign-ICC deployment policies before SQL.
func (p PerspectiveLinesExecutionPolicy) ForPlan(plan PerspectiveLinesPlan) (json.RawMessage, error) {
	if _, err := CompilePerspectiveLinesPlan(plan.scope, plan.image, plan.workingICC); err != nil {
		return nil, err
	}
	var body perspectiveLinesExecutionPolicyBody
	if json.Unmarshal(p.body, &body) != nil {
		return nil, v2input.ErrInvalid
	}
	exact, err := CompilePerspectiveLinesExecutionPolicy(body.WorkingICC, body.RuntimeLockSHA)
	if err != nil || !bytes.Equal(p.body, exact.body) || p.sha != exact.sha || p.workingICC != exact.workingICC || p.workingICC != plan.workingICC {
		return nil, v2input.ErrInvalid
	}
	return p.Bytes(), nil
}
