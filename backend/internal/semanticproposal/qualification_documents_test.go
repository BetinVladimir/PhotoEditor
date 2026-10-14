package semanticproposal

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestQualificationDocumentsBindServerPolicyAndExactBytes(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/implementation-v2/contracts/structural-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Fixtures map[string]json.RawMessage `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(fixtures.Fixtures["Calibration"], &values); err != nil {
		t.Fatal(err)
	}
	values["qualified"] = true
	values["loss_weights"] = []float64{1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6}
	calibration, _ := canonical.Marshal(values)
	calibrationSHA, _ := canonical.Hash(values)
	sha := func(s string) string { return strings.Repeat(s, 64) }
	issuer := uuid.New()
	report := v2qualification.Report{SchemaVersion: 1, Operation: "semantic.transfer", Version: "1", ModelLockSHA: values["model_lock_sha"].(string), RuntimeLockSHA: sha("b"), DeviceProfileSHA: sha("c"), DatasetSHA: values["dataset_sha"].(string), InputFixtureSHAs: []string{sha("e")}, Checks: []v2qualification.Check{{ID: "quality", Passed: true, EvidenceSHA: sha("f")}}, Metrics: v2qualification.Metrics{P50MS: 10, P95MS: 20}, Status: "passed", IssuerID: issuer, CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
	reportBody, _ := canonical.Marshal(report)
	reportSHA, _ := canonical.Hash(report)
	expected := v2qualification.Expected{Operation: report.Operation, Version: report.Version, Backend: "cuda", ModelLockSHA: report.ModelLockSHA, RuntimeLockSHA: report.RuntimeLockSHA, DeviceProfileSHA: report.DeviceProfileSHA, DatasetSHA: report.DatasetSHA, IssuerID: issuer, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{sha("e"): {}}, EvidenceSHAs: map[string]struct{}{sha("f"): {}}, CalibrationSHAs: map[string]struct{}{calibrationSHA: {}}, LicenseCleared: true}
	pin := v2input.SemanticQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: reportSHA}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: calibrationSHA}, ModelLockSHA: expected.ModelLockSHA, RuntimeLockSHA: expected.RuntimeLockSHA, DeviceProfileSHA: expected.DeviceProfileSHA, DatasetSHA: expected.DatasetSHA, IssuerID: issuer, Backend: "cuda"}
	pin.PolicySHA, err = v2input.SemanticQualificationPolicyHash(pin.PolicyID, expected)
	if err != nil {
		t.Fatal(err)
	}
	capacity := TransferPreparation{Color: TransferColorProfile{sha: sha("a")}, RAM: 1 << 30, VRAM: 1 << 30, Scratch: 1 << 30, Output: 1 << 20, MetadataBytes: 16 << 20}
	selection := nativeTransferQualificationSelection{pin: pin, expected: expected, metrics: v2qualification.Metrics{PeakRSSBytes: 100.1, PeakVRAMBytes: 200.2, ScratchBytes: 300.3}}
	assembled, err := applyNativeTransferQualificationSelection(capacity, selection)
	if err != nil || assembled.Qualification != pin || assembled.Costs != (v2input.SemanticQualifiedCosts{RAMBytes: 101, VRAMBytes: 201, ScratchBytes: 301}) || capacity.Qualification != (v2input.SemanticQualificationPin{}) {
		t.Fatal("verified qualification/runtime cost assembly refused or mutated base", err)
	}
	for _, mode := range []string{"qualification", "expected", "cost", "capacity"} {
		changed := ownTransferPreparation(capacity)
		switch mode {
		case "qualification":
			changed.Qualification = pin
			changed.Qualification.Report.ArtifactID = uuid.New()
		case "expected":
			changed.Expected = ownQualificationExpected(expected)
			changed.Expected.ModelLockSHA = sha("9")
		case "cost":
			changed.Costs = v2input.SemanticQualifiedCosts{RAMBytes: 100, VRAMBytes: 201, ScratchBytes: 301}
		case "capacity":
			changed.VRAM = 100
		}
		if denied, err := applyNativeTransferQualificationSelection(changed, selection); err == nil || denied.Qualification != (v2input.SemanticQualificationPin{}) {
			t.Fatal("conflicting or insufficient runtime configuration accepted", mode)
		}
	}
	pinBody, err := canonical.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := decodeNativeTransferQualificationCandidate(pinBody, expected); err != nil || decoded != pin {
		t.Fatal("valid transfer qualification locator refused", err)
	}
	for _, field := range []string{"unknown", "policy_sha", "qualification_id", "report", "calibration", "backend"} {
		var changed map[string]any
		if err := json.Unmarshal(pinBody, &changed); err != nil {
			t.Fatal(err)
		}
		changed[field] = nil
		body, err := canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if decoded, err := decodeNativeTransferQualificationCandidate(body, expected); err == nil || decoded != (v2input.SemanticQualificationPin{}) {
			t.Fatal("modified qualification locator accepted", field)
		}
	}
	if err := VerifyQualificationDocuments(pin, expected, reportBody, calibration); err != nil {
		t.Fatal("valid documents", err)
	}

	// Resource bounds must be derived from the verified original document, not
	// from a second independent metrics input or an unverified decoded report.
	measuredReport := report
	measuredReport.Metrics.PeakRSSBytes = 100.5
	measuredReport.Metrics.PeakVRAMBytes = 202
	measuredReport.Metrics.ScratchBytes = 302.1
	measuredBody, err := canonical.Marshal(measuredReport)
	if err != nil {
		t.Fatal(err)
	}
	measuredPin := pin
	measuredPin.Report.SHA256, err = canonical.Hash(measuredReport)
	if err != nil {
		t.Fatal(err)
	}
	verifiedReport, err := verifyQualificationDocuments(measuredPin, expected, measuredBody, calibration)
	if err != nil || verifiedReport.Metrics != measuredReport.Metrics {
		t.Fatal("verified report metrics lost", err)
	}
	measuredCosts := v2input.SemanticQualifiedCosts{RAMBytes: 101, VRAMBytes: 202, ScratchBytes: 303}
	if err := validateTransferMeasuredCosts(measuredCosts, verifiedReport.Metrics); err != nil {
		t.Fatal("verified measured bounds rejected", err)
	}
	measuredCosts.VRAMBytes--
	if err := validateTransferMeasuredCosts(measuredCosts, verifiedReport.Metrics); err != v2input.ErrInvalid {
		t.Fatal("verified report understated VRAM accepted", err)
	}
	rejectedReport, err := verifyQualificationDocuments(pin, expected, measuredBody, calibration)
	if err == nil || rejectedReport.SchemaVersion != 0 || len(rejectedReport.Checks) != 0 {
		t.Fatal("unverified changed report exposed metrics", err)
	}
	if err := VerifyQualificationDocuments(pin, expected, append(append([]byte(nil), reportBody...), ' '), calibration); err == nil {
		t.Fatal("changed report")
	}
	if err := VerifyQualificationDocuments(pin, expected, reportBody, append(append([]byte(nil), calibration...), ' ')); err == nil {
		t.Fatal("changed calibration")
	}
	changed := pin
	changed.PolicySHA = sha("0")
	if err := VerifyQualificationDocuments(changed, expected, reportBody, calibration); err == nil {
		t.Fatal("foreign policy")
	}
	changed = pin
	changed.Calibration.ArtifactID = pin.Report.ArtifactID
	if err := VerifyQualificationDocuments(changed, expected, reportBody, calibration); err == nil {
		t.Fatal("aliased artifacts")
	}
}
