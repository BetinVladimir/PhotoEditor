package v2optical

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// VerifiedPerspectiveQualification owns original evidence and operator-provisioned
// policy expectations. It is not an authorization token: live SQL authority and
// exact object locations must still be checked in the admission transaction.
type VerifiedPerspectiveQualification struct {
	pin                       PerspectiveQualificationPin
	paramsRaw                 string
	inputICC                  string
	locks                     v2qualification.DeploymentLocks
	reportRaw, calibrationRaw []byte
}

func VerifyPerspectiveQualificationEvidence(pin PerspectiveQualificationPin, locks v2qualification.DeploymentLocks,
	reportRaw, calibrationRaw []byte, params PerspectiveParams, inputICC string) (VerifiedPerspectiveQualification, error) {
	var zero VerifiedPerspectiveQualification
	locks.RequiredChecks = append([]string(nil), locks.RequiredChecks...)
	locks.CalibrationSHAs = append([]string(nil), locks.CalibrationSHAs...)
	locks.TestFixtureSHAs = append([]string(nil), locks.TestFixtureSHAs...)
	locks.EvidenceSHAs = append([]string(nil), locks.EvidenceSHAs...)
	body, _, err := params.Compile()
	if err != nil {
		return zero, err
	}
	value := VerifiedPerspectiveQualification{pin: pin, paramsRaw: string(body), inputICC: inputICC, locks: locks, reportRaw: append([]byte(nil), reportRaw...), calibrationRaw: append([]byte(nil), calibrationRaw...)}
	if err := value.Verify(); err != nil {
		return zero, err
	}
	return value, nil
}

func (v VerifiedPerspectiveQualification) Verify() error {
	if _, err := v.pin.Compile(); err != nil {
		return err
	}
	expected, err := v.locks.Expected()
	if err != nil {
		return err
	}
	p := v.pin
	policySHA, err := PerspectivePolicySHA(p.PolicyID, v.locks)
	if err != nil {
		return err
	}
	if policySHA != p.PolicySHA {
		return v2qualification.ErrUnqualified
	}
	if expected.Operation != "optical.perspective" || expected.Version != "1" || expected.Backend != p.Backend || expected.ModelLockSHA != p.ModelLockSHA || expected.RuntimeLockSHA != p.RuntimeLockSHA || expected.DeviceProfileSHA != p.DeviceProfileSHA || expected.DatasetSHA != p.DatasetSHA || expected.IssuerID != p.IssuerID {
		return v2qualification.ErrUnqualified
	}
	if _, ok := expected.CalibrationSHAs[p.Calibration.SHA256]; !ok {
		return v2qualification.ErrUnqualified
	}
	if _, err := v2qualification.Verify(v.reportRaw, p.Report.SHA256, expected); err != nil {
		return err
	}
	if _, err := ParsePerspectiveParams([]byte(v.paramsRaw)); err != nil {
		return err
	}
	if err := v2input.VerifySemanticCalibration(p.Calibration, v.calibrationRaw, p.ModelLockSHA, p.DatasetSHA); err != nil {
		return err
	}
	var value any
	if json.Unmarshal(v.calibrationRaw, &value) != nil {
		return v2qualification.ErrUnqualified
	}
	canonicalBody, err := canonical.Marshal(value)
	if err != nil || !bytes.Equal(canonicalBody, v.calibrationRaw) {
		return v2qualification.ErrUnqualified
	}
	if _, err := domain.ParseDigest(v.inputICC); err != nil {
		return err
	}

	return nil
}

func (v VerifiedPerspectiveQualification) Pin() (PerspectiveQualificationPin, error) {
	if err := v.Verify(); err != nil {
		return PerspectiveQualificationPin{}, err
	}
	return v.pin, nil
}

func (v VerifiedPerspectiveQualification) CalibrationBytes() ([]byte, error) {
	if err := v.Verify(); err != nil {
		return nil, err
	}
	return append([]byte(nil), v.calibrationRaw...), nil
}
