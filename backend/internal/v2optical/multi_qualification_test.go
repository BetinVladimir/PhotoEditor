package v2optical

import (
	"encoding/json"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func multiQualifiedFixture(t *testing.T, operation string) (MultiQualificationPin, v2qualification.DeploymentLocks, []byte, []byte, []byte, [][]byte) {
	t.Helper()
	read := func(path string) []byte {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var fixture struct {
		Pin    MultiQualificationPin
		Locks  v2qualification.DeploymentLocks
		Report string
	}
	if err := json.Unmarshal(read("testdata/negative-qualification.json"), &fixture); err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(fixture.Report), &report); err != nil {
		t.Fatal(err)
	}
	report["operation"] = operation
	reportRaw, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Pin.Report.SHA256, err = canonical.Hash(report)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Locks.Operation = operation
	var structural struct {
		Fixtures map[string]json.RawMessage `json:"fixtures"`
	}
	if err := json.Unmarshal(read("../../../docs/implementation-v2/contracts/structural-fixtures.json"), &structural); err != nil {
		t.Fatal(err)
	}
	var calibration map[string]any
	if err := json.Unmarshal(structural.Fixtures["Calibration"], &calibration); err != nil {
		t.Fatal(err)
	}
	calibration["qualified"] = true
	calibration["model_lock_sha"] = fixture.Pin.ModelLockSHA
	calibration["dataset_sha"] = fixture.Pin.DatasetSHA
	calibrationRaw, err := canonical.Marshal(calibration)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Pin.Calibration.SHA256, err = canonical.Hash(calibration)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Locks.CalibrationSHAs = []string{fixture.Pin.Calibration.SHA256}
	fixture.Pin.PolicySHA, err = MultiPolicySHA(fixture.Pin.PolicyID, fixture.Locks)
	if err != nil {
		t.Fatal(err)
	}
	p, originals := multiOriginals(t)
	p.Calibration = fixture.Pin.Calibration
	var paramsRaw []byte
	if operation == "optical.panorama" {
		paramsRaw, _, err = p.Compile()
	} else {
		f := FocusStackParams{SchemaVersion: 1, Frames: p.Frames, ReferenceOrdinal: 1, MaxPixels: 10000, Calibration: p.Calibration, MotionPolicy: "review"}
		paramsRaw, _, err = f.Compile()
	}
	if err != nil {
		t.Fatal(err)
	}
	return fixture.Pin, fixture.Locks, reportRaw, calibrationRaw, paramsRaw, originals
}
func TestMultiQualificationOwnsEveryOriginalAndLock(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
			qualified, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			report[0] = ' '
			calibration[0] = ' '
			params[0] = ' '
			originals[0][0] = ' '
			locks.RequiredChecks[0] = "mutated"
			if err := qualified.Verify(); err != nil {
				t.Fatal("caller mutated owned proof", err)
			}
			copied, err := qualified.CalibrationBytes()
			if err != nil {
				t.Fatal(err)
			}
			copied[0] = ' '
			if err := qualified.Verify(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestMultiQualificationRefusesSubstitution(t *testing.T) {
	for _, fault := range []string{"operation", "runtime", "policy", "calibration", "report", "source_order", "params"} {
		t.Run(fault, func(t *testing.T) {
			operation := "optical.panorama"
			pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
			switch fault {
			case "operation":
				operation = "optical.focus_stack"
			case "runtime":
				pin.RuntimeLockSHA = pin.DatasetSHA
			case "policy":
				pin.PolicySHA = pin.DatasetSHA
			case "calibration":
				calibration = append([]byte(" "), calibration...)
			case "report":
				report = append([]byte(" "), report...)
			case "source_order":
				originals[0], originals[1] = originals[1], originals[0]
			case "params":
				params = append([]byte(" "), params...)
			}
			if _, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals); err == nil {
				t.Fatal("substitution accepted")
			}
		})
	}
}
