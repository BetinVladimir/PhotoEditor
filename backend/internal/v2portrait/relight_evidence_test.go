package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
	"time"
)

func relightQualificationFixture(t *testing.T) (RelightParams, RelightQualificationPin, []byte, []byte, v2qualification.Expected) {
	t.Helper()
	raw, err := os.ReadFile("../../../docs/implementation-v2/contracts/structural-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Fixtures map[string]json.RawMessage `json:"fixtures"`
	}
	if json.Unmarshal(raw, &fixtures) != nil {
		t.Fatal("fixtures")
	}
	var calibration map[string]any
	if json.Unmarshal(fixtures.Fixtures["Calibration"], &calibration) != nil {
		t.Fatal("calibration")
	}
	calibration["qualified"] = true
	calibration["loss_weights"] = []float64{1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6, 1. / 6}
	calibrationRaw, _ := canonical.Marshal(calibration)
	calibrationSHA, _ := canonical.Hash(calibration)
	ref := func(sha string) v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	sha := strings.Repeat("a", 64)
	fixtureSHA := strings.Repeat("b", 64)
	evidenceSHA := strings.Repeat("c", 64)
	pin := RelightQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: sha, Report: ref(sha), Calibration: ref(calibrationSHA), ModelLockSHA: calibration["model_lock_sha"].(string), RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: calibration["dataset_sha"].(string), IssuerID: uuid.New(), Backend: "cpu"}
	p := RelightParams{SchemaVersion: 1, Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(sha), CanvasHash: sha}, Depth: ref(sha), Subject: ref(sha), Protected: ref(sha), Calibration: pin.Calibration, Lights: []RelightLight{{Radius: .5}}}
	report := v2qualification.Report{SchemaVersion: 1, Operation: "image.relight", Version: "1", ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA, InputFixtureSHAs: []string{fixtureSHA}, Checks: []v2qualification.Check{{ID: "protected", Passed: true, EvidenceSHA: evidenceSHA}}, Status: "passed", IssuerID: pin.IssuerID, CreatedAt: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)}
	reportRaw, _ := canonical.Marshal(report)
	pin.Report.SHA256, _ = canonical.Hash(report)
	expected := v2qualification.Expected{Operation: "image.relight", Version: "1", Backend: "cpu", ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA, IssuerID: pin.IssuerID, RequiredChecks: []string{"protected"}, TestFixtureSHAs: map[string]struct{}{fixtureSHA: {}}, EvidenceSHAs: map[string]struct{}{evidenceSHA: {}}, LicenseCleared: true}
	return p, pin, reportRaw, calibrationRaw, expected
}

func TestRelightEvidenceBindsIndependentDeploymentPolicy(t *testing.T) {
	p, pin, reportRaw, calibrationRaw, expected := relightQualificationFixture(t)
	var report v2qualification.Report
	var calibration map[string]any
	if json.Unmarshal(reportRaw, &report) != nil || json.Unmarshal(calibrationRaw, &calibration) != nil {
		t.Fatal("fixture")
	}
	fixtureSHA := report.InputFixtureSHAs[0]
	evidenceSHA := report.Checks[0].EvidenceSHA
	if err := VerifyRelightQualificationEvidence(context.Background(), p, pin, reportRaw, calibrationRaw, expected); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*v2qualification.Expected){
		func(e *v2qualification.Expected) { e.Operation = "portrait.open_eyes" }, func(e *v2qualification.Expected) { e.Backend = "cuda" },
		func(e *v2qualification.Expected) { e.RuntimeLockSHA = strings.Repeat("d", 64) }, func(e *v2qualification.Expected) { e.IssuerID = uuid.New() },
		func(e *v2qualification.Expected) { e.LicenseCleared = false }, func(e *v2qualification.Expected) { e.EvidenceSHAs = nil },
		func(e *v2qualification.Expected) { e.CalibrationSHAs = map[string]struct{}{fixtureSHA: {}} },
	} {
		bad := expected
		mutate(&bad)
		if err := VerifyRelightQualificationEvidence(context.Background(), p, pin, reportRaw, calibrationRaw, bad); err == nil {
			t.Fatal("foreign/untrusted expected accepted")
		}
	}
	badReport := report
	badReport.Checks = []v2qualification.Check{{ID: "protected", Passed: false, EvidenceSHA: evidenceSHA}}
	badRaw, _ := canonical.Marshal(badReport)
	badPin := pin
	badPin.Report.SHA256, _ = canonical.Hash(badReport)
	if err := VerifyRelightQualificationEvidence(context.Background(), p, badPin, badRaw, calibrationRaw, expected); err == nil {
		t.Fatal("fresh-pinned failed report")
	}
	calibration["qualified"] = false
	badCalibration, _ := canonical.Marshal(calibration)
	badPin = pin
	badPin.Calibration.SHA256, _ = canonical.Hash(calibration)
	badParams := p
	badParams.Calibration = badPin.Calibration
	if err := VerifyRelightQualificationEvidence(context.Background(), badParams, badPin, reportRaw, badCalibration, expected); err == nil {
		t.Fatal("unqualified calibration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyRelightQualificationEvidence(ctx, p, pin, reportRaw, calibrationRaw, expected); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestRelightDepthProducerOriginalParams(t *testing.T) {
	p, _, _, _, _ := relightQualificationFixture(t)
	values := map[string]any{"image": p.Base.Source, "model_id": "depth_anything_v2_small", "model_lock_id": strings.Repeat("a", 64), "preprocessing_id": "depth_anything_v2_small_518_v1", "input_size": 518}
	verify := func(value map[string]any) error {
		raw, _ := canonical.Marshal(value)
		sha, _ := canonical.Hash(value)
		return VerifyRelightDepthProducerParams(p, v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, raw)
	}
	if err := verify(values); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{"image": v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Base.Source.SHA256}, "model_id": "other", "model_lock_id": "latest", "preprocessing_id": "other", "input_size": 512, "unknown": true} {
		bad := map[string]any{}
		for k, v := range values {
			bad[k] = v
		}
		bad[key] = value
		if err := verify(bad); err == nil {
			t.Fatal("fresh-pinned invalid original Params", key)
		}
	}
	delete(values, "input_size")
	if err := verify(values); err == nil {
		t.Fatal("missing original field")
	}
}

func TestRelightPersonProducerOriginalParams(t *testing.T) {
	p, _, _, _, _ := relightQualificationFixture(t)
	values := map[string]any{"image": p.Base.Source, "model_id": "lraspp_mobilenet_v3_large", "model_lock_id": strings.Repeat("a", 64), "preprocessing_id": "person_guided_v1", "long_side": 768, "guided_tile": 512}
	verify := func(value map[string]any) error {
		raw, _ := canonical.Marshal(value)
		sha, _ := canonical.Hash(value)
		return VerifyRelightPersonProducerParams(p, v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, raw)
	}
	if err := verify(values); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{"image": v2input.Ref{ArtifactID: uuid.New(), SHA256: p.Base.Source.SHA256}, "model_id": "other", "model_lock_id": "latest", "preprocessing_id": "other", "long_side": 512, "guided_tile": 128, "unknown": true} {
		bad := map[string]any{}
		for k, v := range values {
			bad[k] = v
		}
		bad[key] = value
		if err := verify(bad); err == nil {
			t.Fatal("fresh-pinned invalid original Params", key)
		}
	}
	for _, tile := range []int{256, 512, 1024} {
		values["guided_tile"] = tile
		if err := verify(values); err != nil {
			t.Fatal(tile, err)
		}
	}
	delete(values, "guided_tile")
	if err := verify(values); err == nil {
		t.Fatal("missing original field")
	}
}
