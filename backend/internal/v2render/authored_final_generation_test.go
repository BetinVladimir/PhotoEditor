package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
	"time"
)

func finalGenerationFixture(t *testing.T) ([]byte, v2input.Ref, []byte, v2qualification.DeploymentLocks) {
	t.Helper()
	c := testRenderCalibration()
	c.Qualification.Backend = "cpu"
	for i := range c.Samples {
		c.Samples[i].Provider.VRAMBytes = 0
	}
	q := c.Qualification
	cal, e := canonical.Hash(c)
	if e != nil {
		t.Fatal(e)
	}
	issuer := uuid.New()
	locks := v2qualification.DeploymentLocks{Operation: "render.compose", Version: "2", Backend: "cpu", ModelLockSHA: q.ModelSHA, RuntimeLockSHA: q.RuntimeSHA, DeviceProfileSHA: q.DeviceSHA, DatasetSHA: q.DatasetSHA, IssuerID: issuer.String(), RequiredChecks: []string{AuthoredFinalGenerationCheck, AuthoredFinalCalibrationCheck}, CalibrationSHAs: []string{c.EvidenceSHA}, TestFixtureSHAs: []string{strings.Repeat("7", 64)}, EvidenceSHAs: []string{AuthoredFinalRoot3SchemasSHA, cal}, LicenseCleared: true}
	report := v2qualification.Report{SchemaVersion: 1, Operation: "render.compose", Version: "2", ModelLockSHA: q.ModelSHA, RuntimeLockSHA: q.RuntimeSHA, DeviceProfileSHA: q.DeviceSHA, DatasetSHA: q.DatasetSHA, InputFixtureSHAs: locks.TestFixtureSHAs, Checks: []v2qualification.Check{{ID: AuthoredFinalGenerationCheck, Passed: true, EvidenceSHA: AuthoredFinalRoot3SchemasSHA}, {ID: AuthoredFinalCalibrationCheck, Passed: true, EvidenceSHA: cal}}, Metrics: v2qualification.Metrics{P50MS: 1, P95MS: 2, PeakRSSBytes: 4096, ScratchBytes: 4096}, Status: "passed", IssuerID: issuer, CreatedAt: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)}
	r, e := canonical.Marshal(report)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(r)
	doc := AuthoredFinalGenerationEvidence{SchemaVersion: 1, Operation: "render.compose", Version: "2", ContractsSHA: AuthoredFinalRoot3SchemasSHA, Kernel: "v2.authored.1", Qualification: q, Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(h[:])}, CalibrationSHA: cal, Calibration: c}
	raw, e := canonical.Marshal(doc)
	if e != nil {
		t.Fatal(e)
	}
	h = sha256.Sum256(raw)
	return raw, v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(h[:])}, r, locks
}
func TestFinal338GenerationRequiresActualReportAndOwnsBytes(t *testing.T) {
	raw, ref, report, locks := finalGenerationFixture(t)
	proof, e := VerifyAuthoredFinalGenerationEvidence(context.Background(), raw, ref, report, locks)
	if e != nil {
		t.Fatal(e)
	}
	raw[0] = 'x'
	report[0] = 'x'
	locks.EvidenceSHAs[0] = "bad"
	d, e := proof.Evidence()
	if e != nil || d.ContractsSHA != AuthoredFinalRoot3SchemasSHA {
		t.Fatal("proof aliases", e)
	}
	d.Calibration.Samples[0].Width = 0
	again, e := proof.Evidence()
	if e != nil || again.Calibration.Samples[0].Width == 0 {
		t.Fatal("getter aliases")
	}
}
func TestFinal338GenerationRefusesHistoricalAndForgedBindings(t *testing.T) {
	for _, kind := range []string{"old-schema", "wrong-calibration", "report-bytes", "missing-policy-check", "missing-policy-evidence", "report-old-check", "wrong-runtime", "gpu", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			raw, ref, report, locks := finalGenerationFixture(t)
			var d map[string]any
			json.Unmarshal(raw, &d)
			switch kind {
			case "old-schema":
				d["contracts_sha"] = AuthoredFinalSchemasSHA
			case "wrong-calibration":
				d["calibration_sha256"] = strings.Repeat("1", 64)
			case "report-bytes":
				report[0] = 'x'
			case "missing-policy-check":
				locks.RequiredChecks = locks.RequiredChecks[1:]
			case "missing-policy-evidence":
				locks.EvidenceSHAs = locks.EvidenceSHAs[1:]
			case "report-old-check":
				var r map[string]any
				json.Unmarshal(report, &r)
				r["checks"].([]any)[0].(map[string]any)["id"] = "old55c"
				report, _ = canonical.Marshal(r)
				h := sha256.Sum256(report)
				d["qualification_report"].(map[string]any)["sha256"] = hex.EncodeToString(h[:])
			case "wrong-runtime":
				d["qualification"].(map[string]any)["runtime_lock_sha"] = strings.Repeat("2", 64)
			case "gpu":
				locks.Backend = "mps"
			case "unknown":
				d["trusted"] = true
			}
			raw, _ = canonical.Marshal(d)
			h := sha256.Sum256(raw)
			ref.SHA256 = hex.EncodeToString(h[:])
			if _, e := VerifyAuthoredFinalGenerationEvidence(context.Background(), raw, ref, report, locks); e == nil {
				t.Fatal("accepted forged generation")
			}
		})
	}
}
func TestFinal338GenerationCancellationAndZeroProof(t *testing.T) {
	raw, ref, report, locks := finalGenerationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := VerifyAuthoredFinalGenerationEvidence(ctx, raw, ref, report, locks); e != context.Canceled {
		t.Fatal(e)
	}
	if _, e := (VerifiedAuthoredFinalGeneration{}).Evidence(); e == nil {
		t.Fatal("zero proof")
	}
	if AuthoredFinalSchemasSHA == AuthoredFinalRoot3SchemasSHA {
		t.Fatal("historical pin overwritten")
	}
}
