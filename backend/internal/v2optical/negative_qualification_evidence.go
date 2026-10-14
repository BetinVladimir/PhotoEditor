package v2optical

import (
	"photoeditor/backend/internal/v2qualification"
)

// VerifiedNegativeQualification owns original evidence and operator-provisioned
// policy expectations. It is not an authorization token: live SQL authority and
// exact object locations must still be checked in the admission transaction.
type VerifiedNegativeQualification struct {
	pin                       NegativeQualificationPin
	params                    NegativeParams
	inputICC                  string
	locks                     v2qualification.DeploymentLocks
	reportRaw, calibrationRaw []byte
}

func VerifyNegativeQualificationEvidence(pin NegativeQualificationPin, locks v2qualification.DeploymentLocks,
	reportRaw, calibrationRaw []byte, params NegativeParams, inputICC string) (VerifiedNegativeQualification, error) {
	var zero VerifiedNegativeQualification
	locks.RequiredChecks = append([]string(nil), locks.RequiredChecks...)
	locks.CalibrationSHAs = append([]string(nil), locks.CalibrationSHAs...)
	locks.TestFixtureSHAs = append([]string(nil), locks.TestFixtureSHAs...)
	locks.EvidenceSHAs = append([]string(nil), locks.EvidenceSHAs...)
	value := VerifiedNegativeQualification{pin: pin, params: params, inputICC: inputICC, locks: locks, reportRaw: append([]byte(nil), reportRaw...), calibrationRaw: append([]byte(nil), calibrationRaw...)}
	if err := value.Verify(); err != nil {
		return zero, err
	}
	return value, nil
}

func (v VerifiedNegativeQualification) Verify() error {
	if _, err := v.pin.Compile(); err != nil {
		return err
	}
	expected, err := v.locks.Expected()
	if err != nil {
		return err
	}
	p := v.pin
	policySHA, err := NegativePolicySHA(p.PolicyID, v.locks)
	if err != nil {
		return err
	}
	if policySHA != p.PolicySHA {
		return v2qualification.ErrUnqualified
	}
	if expected.Operation != "optical.negative" || expected.Version != "1" || expected.Backend != p.Backend || expected.ModelLockSHA != p.ModelLockSHA || expected.RuntimeLockSHA != p.RuntimeLockSHA || expected.DeviceProfileSHA != p.DeviceProfileSHA || expected.DatasetSHA != p.DatasetSHA || expected.IssuerID != p.IssuerID {
		return v2qualification.ErrUnqualified
	}
	if _, ok := expected.CalibrationSHAs[p.Calibration.SHA256]; !ok {
		return v2qualification.ErrUnqualified
	}
	if _, err := v2qualification.Verify(v.reportRaw, p.Report.SHA256, expected); err != nil {
		return err
	}
	if v.params.Calibration != p.Calibration {
		return v2qualification.ErrUnqualified
	}
	calibration, err := VerifyNegativeCalibration(v.params, v.calibrationRaw, v.inputICC)
	if err != nil {
		return err
	}
	if calibration.Qualification != p.Report {
		return v2qualification.ErrUnqualified
	}
	return nil
}

func (v VerifiedNegativeQualification) Pin() (NegativeQualificationPin, error) {
	if err := v.Verify(); err != nil {
		return NegativeQualificationPin{}, err
	}
	return v.pin, nil
}

func (v VerifiedNegativeQualification) CalibrationBytes() ([]byte, error) {
	if err := v.Verify(); err != nil {
		return nil, err
	}
	return append([]byte(nil), v.calibrationRaw...), nil
}
