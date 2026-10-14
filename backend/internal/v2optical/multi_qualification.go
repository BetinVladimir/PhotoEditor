package v2optical

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type MultiQualificationPin PerspectiveQualificationPin

func (p MultiQualificationPin) Compile() ([]byte, error) {
	return PerspectiveQualificationPin(p).Compile()
}

// Content evidence only: every admission must re-lock live SQL scope and locators.
type VerifiedMultiQualification struct {
	operation                 string
	pin                       MultiQualificationPin
	paramsRaw                 string
	sourceRaw                 []string
	locks                     v2qualification.DeploymentLocks
	reportRaw, calibrationRaw []byte
}

func VerifyMultiQualificationEvidence(operation string, pin MultiQualificationPin, locks v2qualification.DeploymentLocks,
	reportRaw, calibrationRaw, paramsRaw []byte, originals [][]byte) (VerifiedMultiQualification, error) {
	locks.RequiredChecks = append([]string(nil), locks.RequiredChecks...)
	locks.CalibrationSHAs = append([]string(nil), locks.CalibrationSHAs...)
	locks.TestFixtureSHAs = append([]string(nil), locks.TestFixtureSHAs...)
	locks.EvidenceSHAs = append([]string(nil), locks.EvidenceSHAs...)
	value := VerifiedMultiQualification{operation: operation, pin: pin, locks: locks, paramsRaw: string(paramsRaw),
		reportRaw: append([]byte(nil), reportRaw...), calibrationRaw: append([]byte(nil), calibrationRaw...)}
	for _, raw := range originals {
		value.sourceRaw = append(value.sourceRaw, string(raw))
	}
	if err := value.Verify(); err != nil {
		return VerifiedMultiQualification{}, err
	}
	return value, nil
}
func (v VerifiedMultiQualification) Verify() error {
	if _, err := v.pin.Compile(); err != nil {
		return err
	}
	var calibration v2input.Ref
	switch v.operation {
	case "optical.panorama":
		p, err := ParsePanoramaParams([]byte(v.paramsRaw))
		if err != nil {
			return err
		}
		calibration = p.Calibration
	case "optical.focus_stack":
		p, err := ParseFocusStackParams([]byte(v.paramsRaw))
		if err != nil {
			return err
		}
		calibration = p.Calibration
	default:
		return v2qualification.ErrUnqualified
	}
	if calibration != v.pin.Calibration {
		return v2qualification.ErrUnqualified
	}
	originals := make([][]byte, len(v.sourceRaw))
	for i, raw := range v.sourceRaw {
		originals[i] = []byte(raw)
	}
	if _, err := VerifyMultiWorkingImages(v.operation, []byte(v.paramsRaw), originals); err != nil {
		return err
	}
	expected, err := v.locks.Expected()
	if err != nil {
		return err
	}
	pin := v.pin
	policy, err := MultiPolicySHA(pin.PolicyID, v.locks)
	if err != nil || policy != pin.PolicySHA || expected.Operation != v.operation || expected.Version != "1" ||
		expected.Backend != pin.Backend || expected.ModelLockSHA != pin.ModelLockSHA || expected.RuntimeLockSHA != pin.RuntimeLockSHA ||
		expected.DeviceProfileSHA != pin.DeviceProfileSHA || expected.DatasetSHA != pin.DatasetSHA || expected.IssuerID != pin.IssuerID {
		return v2qualification.ErrUnqualified
	}
	if _, ok := expected.CalibrationSHAs[pin.Calibration.SHA256]; !ok {
		return v2qualification.ErrUnqualified
	}
	report, err := v2qualification.Verify(v.reportRaw, pin.Report.SHA256, expected)
	if err != nil {
		return err
	}
	if report.Metrics.PeakVRAMBytes != 0 {
		return v2qualification.ErrUnqualified
	}
	if err := v2input.VerifySemanticCalibration(pin.Calibration, v.calibrationRaw, pin.ModelLockSHA, pin.DatasetSHA); err != nil {
		return err
	}
	var value any
	if json.Unmarshal(v.calibrationRaw, &value) != nil {
		return v2qualification.ErrUnqualified
	}
	body, err := canonical.Marshal(value)
	if err != nil || !bytes.Equal(body, v.calibrationRaw) {
		return v2qualification.ErrUnqualified
	}
	return nil
}
func (v VerifiedMultiQualification) Pin() (MultiQualificationPin, error) {
	if err := v.Verify(); err != nil {
		return MultiQualificationPin{}, err
	}
	return v.pin, nil
}
func (v VerifiedMultiQualification) CalibrationBytes() ([]byte, error) {
	if err := v.Verify(); err != nil {
		return nil, err
	}
	return append([]byte(nil), v.calibrationRaw...), nil
}
