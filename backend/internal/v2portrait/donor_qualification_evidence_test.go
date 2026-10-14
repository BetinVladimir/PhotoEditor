package v2portrait

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
	"time"
)

func TestVerifiedDonorQualificationEvidence(t *testing.T) {
	model, dataset := strings.Repeat("a", 64), strings.Repeat("b", 64)
	raw, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "model_lock_sha": model, "dataset_sha": dataset, "qualified": true,
		"scene_cosine_min": .3, "scene_log_luma_max": 1., "cluster_diameter_max": 1.,
		"sharpness_scale": 1., "clipping_penalty": 1., "match_inlier_min": .3, "match_error_px_max": 5.,
		"identity_similarity_min": .4, "landmark_drift_max": .2,
		"normalization_scales": []float64{1, 1, 1, 1, 1, 1}, "loss_weights": []float64{.4, .1, .1, .1, .2, .1},
		"focus_warning": .5, "focus_critical": .2, "eye_open_min": .5, "glare_warning_fraction": .1,
		"clipping_warning_fraction": .1, "clipping_critical_fraction": .3, "min_face_edge_px": 64,
		"depth_sigma": .1, "registration_error_px": 2., "motion_threshold": .1,
	})
	if err != nil {
		t.Fatal(err)
	}
	calibrationSHA, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	runtime, device, fixture, evidence := strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("f", 64)
	issuer := uuid.New()
	locks := v2qualification.DeploymentLocks{Operation: "portrait.open_eyes", Version: "1", Backend: "cpu", ModelLockSHA: model, RuntimeLockSHA: runtime, DeviceProfileSHA: device, DatasetSHA: dataset, IssuerID: issuer.String(), RequiredChecks: []string{"identity"}, CalibrationSHAs: []string{calibrationSHA}, TestFixtureSHAs: []string{fixture}, EvidenceSHAs: []string{evidence}, LicenseCleared: true}
	report := v2qualification.Report{SchemaVersion: 1, Operation: locks.Operation, Version: "1", ModelLockSHA: model, RuntimeLockSHA: runtime, DeviceProfileSHA: device, DatasetSHA: dataset, InputFixtureSHAs: []string{fixture}, Checks: []v2qualification.Check{{ID: "identity", Passed: true, EvidenceSHA: evidence}}, Metrics: v2qualification.Metrics{P50MS: 1, P95MS: 2, PeakRSSBytes: 1024, ScratchBytes: 1024}, Status: "passed", IssuerID: issuer, CreatedAt: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)}
	reportRaw, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	reportSHA, err := canonical.Hash(json.RawMessage(reportRaw))
	if err != nil {
		t.Fatal(err)
	}
	pin := DonorQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: issuer, PolicySHA: model, Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: reportSHA}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: calibrationSHA}, ModelLockSHA: model, RuntimeLockSHA: runtime, DeviceProfileSHA: device, DatasetSHA: dataset, Backend: "cpu"}
	pin.PolicySHA, err = DonorPolicySHA(pin.PolicyID, locks)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyDonorQualificationEvidence(pin, locks, reportRaw, raw)
	if err != nil {
		t.Fatal(err)
	}
	checkLocatedDonorQualification(t, verified)
	retained, err := verified.Pin()
	if err != nil || retained != pin {
		t.Fatal("pin", err)
	}
	body, err := verified.CalibrationBytes()
	if err != nil {
		t.Fatal(err)
	}
	body[0] ^= 1
	if err := verified.Verify(); err != nil {
		t.Fatal("accessor exposed mutable evidence", err)
	}
	for _, mode := range []string{"report", "calibration", "runtime", "allowlist", "check", "license", "operation"} {
		p, l, r, c := pin, locks, append([]byte(nil), reportRaw...), append([]byte(nil), raw...)
		switch mode {
		case "report":
			r[0] ^= 1
		case "calibration":
			c[0] ^= 1
		case "runtime":
			p.RuntimeLockSHA = model
		case "allowlist":
			l.CalibrationSHAs = []string{model}
		case "check":
			l.RequiredChecks = []string{"missing"}
		case "license":
			l.LicenseCleared = false
		case "operation":
			l.Operation = "portrait.hair"
		}
		got, err := VerifyDonorQualificationEvidence(p, l, r, c)
		if err == nil || got.reportRaw != nil || got.calibrationRaw != nil {
			t.Fatal("invalid evidence escaped", mode)
		}
	}
	reportRaw[0] ^= 1
	raw[0] ^= 1
	locks.RequiredChecks[0] = "changed"
	locks.CalibrationSHAs[0] = model
	if err := verified.Verify(); err != nil {
		t.Fatal("caller changed retained evidence", err)
	}
	if _, err := (VerifiedDonorQualification{}).Pin(); err == nil {
		t.Fatal("zero evidence accepted")
	}
}
