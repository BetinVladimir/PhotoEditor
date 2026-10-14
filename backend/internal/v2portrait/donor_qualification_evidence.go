package v2portrait

import (
	"photoeditor/backend/internal/v2calibration"
	"photoeditor/backend/internal/v2qualification"
)

// VerifiedDonorQualification owns original evidence and operator-provisioned
// policy expectations. It is not an authorization token: live SQL authority and
// exact object locations must still be checked in the admission transaction.
type VerifiedDonorQualification struct {
	pin                       DonorQualificationPin
	locks                     v2qualification.DeploymentLocks
	reportRaw, calibrationRaw []byte
}

func VerifyDonorQualificationEvidence(pin DonorQualificationPin, locks v2qualification.DeploymentLocks,
	reportRaw, calibrationRaw []byte) (VerifiedDonorQualification, error) {
	var zero VerifiedDonorQualification
	locks.RequiredChecks = append([]string(nil), locks.RequiredChecks...)
	locks.CalibrationSHAs = append([]string(nil), locks.CalibrationSHAs...)
	locks.TestFixtureSHAs = append([]string(nil), locks.TestFixtureSHAs...)
	locks.EvidenceSHAs = append([]string(nil), locks.EvidenceSHAs...)
	value := VerifiedDonorQualification{pin: pin, locks: locks, reportRaw: append([]byte(nil), reportRaw...), calibrationRaw: append([]byte(nil), calibrationRaw...)}
	if err := value.Verify(); err != nil {
		return zero, err
	}
	return value, nil
}

func (v VerifiedDonorQualification) Verify() error {
	if _, err := v.pin.Compile(); err != nil {
		return err
	}
	expected, err := v.locks.Expected()
	if err != nil {
		return err
	}
	p := v.pin
	policySHA, err := DonorPolicySHA(p.PolicyID, v.locks)
	if err != nil {
		return err
	}
	if policySHA != p.PolicySHA {
		return v2qualification.ErrUnqualified
	}
	if expected.Operation != "portrait.open_eyes" || expected.Version != "1" || expected.Backend != p.Backend || expected.ModelLockSHA != p.ModelLockSHA || expected.RuntimeLockSHA != p.RuntimeLockSHA || expected.DeviceProfileSHA != p.DeviceProfileSHA || expected.DatasetSHA != p.DatasetSHA || expected.IssuerID != p.IssuerID {
		return v2qualification.ErrUnqualified
	}
	if _, ok := expected.CalibrationSHAs[p.Calibration.SHA256]; !ok {
		return v2qualification.ErrUnqualified
	}
	if _, err := v2qualification.Verify(v.reportRaw, p.Report.SHA256, expected); err != nil {
		return err
	}
	_, err = v2calibration.CalibrationMinFaceEdge(v.calibrationRaw, p.Calibration.SHA256, p.ModelLockSHA, p.DatasetSHA)
	return err
}

func (v VerifiedDonorQualification) Pin() (DonorQualificationPin, error) {
	if err := v.Verify(); err != nil {
		return DonorQualificationPin{}, err
	}
	return v.pin, nil
}

func (v VerifiedDonorQualification) CalibrationBytes() ([]byte, error) {
	if err := v.Verify(); err != nil {
		return nil, err
	}
	return append([]byte(nil), v.calibrationRaw...), nil
}
