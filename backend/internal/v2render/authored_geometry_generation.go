package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

const AuthoredGeometryGenerationCheck = "authored_geometry_schema_" + AuthoredGeometrySchemasSHA
const AuthoredGeometryCalibrationCheck = "authored_geometry_calibration"

// A separate versioned artifact, never a changed serialization of historical
// qualification reports or a default optional field in the old55c profile.
type AuthoredGeometryGenerationEvidence struct {
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
type VerifiedAuthoredGeometryGeneration struct {
	reference   v2input.Ref
	documentJCS string
	reportJCS   string
	documents   []v2draft.DocumentWitness
}

func (p VerifiedAuthoredGeometryGeneration) Ref() v2input.Ref { return p.reference }
func (p VerifiedAuthoredGeometryGeneration) Evidence() (AuthoredGeometryGenerationEvidence, error) {
	var d AuthoredGeometryGenerationEvidence
	if p.documentJCS == "" || decodeAuthoredParams([]byte(p.documentJCS), &d) != nil {
		return d, v2input.ErrInvalid
	}
	return d, nil
}

// locks comes from the authenticated operator deployment policy, never the
// document/report. Existing Verify enforces issuer, exact pins, measurements,
// fixtures/checks/evidence sets and passed status. Both new required checks
// must be explicitly present in that trusted policy and actual new report.
func VerifyAuthoredGeometryGenerationEvidence(ctx context.Context, raw []byte, ref v2input.Ref, reportRaw []byte, locks v2qualification.DeploymentLocks) (VerifiedAuthoredGeometryGeneration, error) {
	bad := func() (VerifiedAuthoredGeometryGeneration, error) {
		return VerifiedAuthoredGeometryGeneration{}, v2input.ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return VerifiedAuthoredGeometryGeneration{}, e
	}
	if len(raw) < 2 || len(raw) > 1048576 || len(reportRaw) < 2 || len(reportRaw) > 4194304 || !authoredGenerationRefValid(ref) {
		return bad()
	}
	sum := sha256.Sum256(raw)
	wire, e := canonical.Bytes(raw)
	if e != nil || !bytes.Equal(wire, raw) || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return bad()
	}
	var d AuthoredGeometryGenerationEvidence
	if decodeAuthoredParams(raw, &d) != nil || d.SchemaVersion != 1 || d.Operation != "render.authored_geometry" || d.Version != "1" || d.ContractsSHA != AuthoredGeometrySchemasSHA || d.Kernel != AuthoredGeometryKernelVersion || d.Qualification.Backend != "cpu" || !authoredGenerationRefValid(d.Report) || d.Report.ArtifactID == ref.ArtifactID {
		return bad()
	}
	round, e := canonical.Marshal(d)
	if e != nil || !bytes.Equal(round, raw) {
		return bad()
	}
	expected, e := locks.Expected()
	if e != nil || expected.Operation != "render.authored_geometry" || expected.Version != "1" || expected.Backend != "cpu" {
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
	if !required[AuthoredGeometryGenerationCheck] || !required[AuthoredGeometryCalibrationCheck] {
		return bad()
	}
	if _, ok := expected.EvidenceSHAs[AuthoredGeometrySchemasSHA]; !ok {
		return bad()
	}
	if _, ok := expected.EvidenceSHAs[calibrationSHA]; !ok {
		return bad()
	}
	report, e := v2qualification.Verify(reportRaw, d.Report.SHA256, expected)
	if e != nil {
		return VerifiedAuthoredGeometryGeneration{}, e
	}
	foundSchema, foundCalibration := false, false
	for _, check := range report.Checks {
		if check.ID == AuthoredGeometryGenerationCheck && check.EvidenceSHA == AuthoredGeometrySchemasSHA {
			foundSchema = true
		}
		if check.ID == AuthoredGeometryCalibrationCheck && check.EvidenceSHA == calibrationSHA {
			foundCalibration = true
		}
	}
	if !foundSchema || !foundCalibration || report.Metrics.PeakVRAMBytes != 0 {
		return bad()
	}
	if e = ctx.Err(); e != nil {
		return VerifiedAuthoredGeometryGeneration{}, e
	}
	return VerifiedAuthoredGeometryGeneration{reference: ref, documentJCS: string(append([]byte(nil), raw...)), reportJCS: string(append([]byte(nil), reportRaw...))}, nil
}

// Only this private catalog can be produced from verified generation/report
// bytes; the production preparer must not substitute historical raw calibration.
type verifiedAuthoredGeometryGenerationCatalog struct {
	proof VerifiedAuthoredGeometryGeneration
}

func (c verifiedAuthoredGeometryGenerationCatalog) ResolveAuthoredGeometry(ctx context.Context, q RenderQualification) (RenderCalibration, error) {
	if e := ctx.Err(); e != nil {
		return RenderCalibration{}, e
	}
	d, e := c.proof.Evidence()
	if e != nil {
		return RenderCalibration{}, e
	}
	if d.Qualification != q {
		return RenderCalibration{}, renderUnavailable()
	}
	d.Calibration.Samples = append([]RenderCalibrationSample(nil), d.Calibration.Samples...)
	return d.Calibration, nil
}
func CompileVerifiedAuthoredGeometryExecutionProfile(ctx context.Context, proof VerifiedAuthoredGeometryGeneration, width, height int64, icc string) ([]byte, error) {
	d, e := proof.Evidence()
	if e != nil {
		return nil, e
	}
	return CompileAuthoredGeometryExecutionProfile(ctx, verifiedAuthoredGeometryGenerationCatalog{proof}, d.Qualification, width, height, icc)
}
