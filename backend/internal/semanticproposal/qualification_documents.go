package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// verifyQualificationConfiguration binds deployment policy and immutable refs
// before any publication or catalog IO. It does not verify document bytes.
func verifyQualificationConfiguration(pin v2input.SemanticQualificationPin, expected v2qualification.Expected) error {
	policySHA, err := v2input.SemanticQualificationPolicyHash(pin.PolicyID, expected)
	if err != nil || pin.QualificationID == uuid.Nil || pin.Report.ArtifactID == uuid.Nil || pin.Calibration.ArtifactID == uuid.Nil || pin.Report.ArtifactID == pin.Calibration.ArtifactID || pin.PolicySHA != policySHA || pin.Backend != expected.Backend || pin.ModelLockSHA != expected.ModelLockSHA || pin.RuntimeLockSHA != expected.RuntimeLockSHA || pin.DeviceProfileSHA != expected.DeviceProfileSHA || pin.DatasetSHA != expected.DatasetSHA || pin.IssuerID != expected.IssuerID {
		return v2input.ErrInvalid
	}
	if _, ok := expected.CalibrationSHAs[pin.Calibration.SHA256]; !ok {
		return v2input.ErrInvalid
	}
	return nil
}

// VerifyQualificationDocuments binds exact document bytes to deployment-selected
// locks. Expected must originate from trusted server configuration, never intent.
// Live imported qualification/catalog authority remains a separate SQL check.
func VerifyQualificationDocuments(pin v2input.SemanticQualificationPin, expected v2qualification.Expected, report, calibration []byte) error {
	_, err := verifyQualificationDocuments(pin, expected, report, calibration)
	return err
}
func verifyQualificationDocuments(pin v2input.SemanticQualificationPin, expected v2qualification.Expected, report, calibration []byte) (v2qualification.Report, error) {
	if err := verifyQualificationConfiguration(pin, expected); err != nil {
		return v2qualification.Report{}, err
	}
	if err := v2input.VerifySemanticCalibration(pin.Calibration, calibration, expected.ModelLockSHA, expected.DatasetSHA); err != nil {
		return v2qualification.Report{}, err
	}
	return v2qualification.Verify(report, pin.Report.SHA256, expected)
}
