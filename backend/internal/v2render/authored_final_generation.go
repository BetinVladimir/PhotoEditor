package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

const AuthoredFinalRoot3SchemasSHA = "338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b"
const AuthoredFinalGenerationCheck = "authored_final_schema_" + AuthoredFinalRoot3SchemasSHA
const AuthoredFinalCalibrationCheck = "authored_final_calibration"

// A separate versioned artifact, never a changed serialization of historical
// qualification reports or a default optional field in the old55c profile.
type AuthoredFinalGenerationEvidence struct {
	SchemaVersion  int                 `json:"schema_version"`
	Operation      string              `json:"operation"`
	Version        string              `json:"operation_version"`
	ContractsSHA   string              `json:"contracts_sha"`
	Kernel         string              `json:"authored_kernel_version"`
	Qualification  RenderQualification `json:"qualification"`
	Report         v2input.Ref         `json:"qualification_report"`
	CalibrationSHA string              `json:"calibration_sha256"`
	Calibration    RenderCalibration   `json:"calibration"`
}

// No exported fields/constructor from a boolean: proof owns the canonical
// physical generation/report bytes actually verified below. SQL current
// publication/tenant authority is a separate required reader boundary.
type VerifiedAuthoredFinalGeneration struct {
	reference   v2input.Ref
	documentJCS string
	reportJCS   string
	documents   []v2draft.DocumentWitness
}

func (p VerifiedAuthoredFinalGeneration) Ref() v2input.Ref { return p.reference }
func (p VerifiedAuthoredFinalGeneration) Evidence() (AuthoredFinalGenerationEvidence, error) {
	var d AuthoredFinalGenerationEvidence
	if p.documentJCS == "" || decodeAuthoredParams([]byte(p.documentJCS), &d) != nil {
		return d, v2input.ErrInvalid
	}
	return d, nil
}

// locks comes from the authenticated operator deployment policy, never the
// document/report. Existing Verify enforces issuer, exact pins, measurements,
// fixtures/checks/evidence sets and passed status. Both new required checks
// must be explicitly present in that trusted policy and actual new report.
func VerifyAuthoredFinalGenerationEvidence(ctx context.Context, raw []byte, ref v2input.Ref, reportRaw []byte, locks v2qualification.DeploymentLocks) (VerifiedAuthoredFinalGeneration, error) {
	bad := func() (VerifiedAuthoredFinalGeneration, error) {
		return VerifiedAuthoredFinalGeneration{}, v2input.ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return VerifiedAuthoredFinalGeneration{}, e
	}
	if len(raw) < 2 || len(raw) > 1048576 || len(reportRaw) < 2 || len(reportRaw) > 4194304 || !authoredGenerationRefValid(ref) {
		return bad()
	}
	sum := sha256.Sum256(raw)
	wire, e := canonical.Bytes(raw)
	if e != nil || !bytes.Equal(wire, raw) || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return bad()
	}
	var d AuthoredFinalGenerationEvidence
	if decodeAuthoredParams(raw, &d) != nil || d.SchemaVersion != 1 || d.Operation != "render.compose" || d.Version != "2" || d.ContractsSHA != AuthoredFinalRoot3SchemasSHA || d.Kernel != "v2.authored.1" || d.Qualification.Backend != "cpu" || !authoredGenerationRefValid(d.Report) || d.Report.ArtifactID == ref.ArtifactID {
		return bad()
	}
	round, e := canonical.Marshal(d)
	if e != nil || !bytes.Equal(round, raw) {
		return bad()
	}
	expected, e := locks.Expected()
	if e != nil || expected.Operation != "render.compose" || expected.Version != "2" || expected.Backend != "cpu" {
		return bad()
	}
	q := RenderQualification{ModelSHA: expected.ModelLockSHA, RuntimeSHA: expected.RuntimeLockSHA, Backend: expected.Backend, DeviceSHA: expected.DeviceProfileSHA, DatasetSHA: expected.DatasetSHA}
	if q != d.Qualification || d.Calibration.validate(q) != nil {
		return bad()
	}
	calibrationSHA, e := canonical.Hash(d.Calibration)
	if e != nil || calibrationSHA != d.CalibrationSHA {
		return bad()
	}
	required := map[string]bool{}
	for _, id := range expected.RequiredChecks {
		required[id] = true
	}
	if !required[AuthoredFinalGenerationCheck] || !required[AuthoredFinalCalibrationCheck] {
		return bad()
	}
	if _, ok := expected.EvidenceSHAs[AuthoredFinalRoot3SchemasSHA]; !ok {
		return bad()
	}
	if _, ok := expected.EvidenceSHAs[calibrationSHA]; !ok {
		return bad()
	}
	report, e := v2qualification.Verify(reportRaw, d.Report.SHA256, expected)
	if e != nil {
		return VerifiedAuthoredFinalGeneration{}, e
	}
	foundSchema, foundCalibration := false, false
	for _, check := range report.Checks {
		if check.ID == AuthoredFinalGenerationCheck && check.EvidenceSHA == AuthoredFinalRoot3SchemasSHA {
			foundSchema = true
		}
		if check.ID == AuthoredFinalCalibrationCheck && check.EvidenceSHA == calibrationSHA {
			foundCalibration = true
		}
	}
	if !foundSchema || !foundCalibration || report.Metrics.PeakVRAMBytes != 0 {
		return bad()
	}
	if e = ctx.Err(); e != nil {
		return VerifiedAuthoredFinalGeneration{}, e
	}
	return VerifiedAuthoredFinalGeneration{reference: ref, documentJCS: string(append([]byte(nil), raw...)), reportJCS: string(append([]byte(nil), reportRaw...))}, nil
}

// Distinct profile shape: old55c builders/readers cannot silently consume it.
type AuthoredFinalRoot3ExecutionProfile struct {
	AuthoredFinalExecutionProfile
	GenerationEvidence v2input.Ref `json:"generation_evidence"`
}

func CompileAuthoredFinalRoot3ExecutionProfile(ctx context.Context, request RenderDraftRequest, params CompiledAuthoredFinalParams, targetRaw []byte, proof VerifiedAuthoredFinalGeneration) (json.RawMessage, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	d, e := proof.Evidence()
	if e != nil || proof.reportJCS == "" || !authoredGenerationRefValid(proof.reference) {
		return nil, v2input.ErrInvalid
	}
	// Reuse the unchanged actual CPU orchestration/resource formula. This is
	// permitted only after NEW generation-qualified report/calibration proof,
	// never by replacing the contracts pin on an old measured catalog entry.
	raw, e := CompileAuthoredFinalExecutionProfile(request, params, targetRaw, d.Qualification, d.Calibration)
	if e != nil {
		return nil, e
	}
	var base AuthoredFinalExecutionProfile
	if decodeAuthoredParams(raw, &base) != nil {
		return nil, v2input.ErrInvalid
	}
	base.ContractsSHA = AuthoredFinalRoot3SchemasSHA
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return canonical.Marshal(AuthoredFinalRoot3ExecutionProfile{AuthoredFinalExecutionProfile: base, GenerationEvidence: proof.reference})
}

func authoredGenerationRefValid(r v2input.Ref) bool {
	_, e := domain.ParseDigest(r.SHA256)
	return r.ArtifactID != uuid.Nil && e == nil
}
