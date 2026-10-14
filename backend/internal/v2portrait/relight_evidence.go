package v2portrait

import (
	"bytes"
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// VerifyRelightQualificationEvidence requires deployment-controlled expected
// locks and fixture/evidence sets, independently of the request and report.
// It does not import qualification or establish current SQL catalog authority.
func VerifyRelightQualificationEvidence(ctx context.Context, params RelightParams, pin RelightQualificationPin,
	reportRaw, calibrationRaw []byte, expected v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := RelightRootInputs(params, pin); err != nil {
		return err
	}
	if expected.Operation != "image.relight" || expected.Version != "1" || expected.Backend != "cpu" ||
		expected.ModelLockSHA != pin.ModelLockSHA || expected.RuntimeLockSHA != pin.RuntimeLockSHA ||
		expected.DeviceProfileSHA != pin.DeviceProfileSHA || expected.DatasetSHA != pin.DatasetSHA || expected.IssuerID != pin.IssuerID {
		return v2qualification.ErrUnqualified
	}
	if _, err := v2qualification.Verify(reportRaw, pin.Report.SHA256, expected); err != nil {
		return err
	}
	normalized, err := canonical.Bytes(calibrationRaw)
	if err != nil || !bytes.Equal(normalized, calibrationRaw) {
		return v2input.ErrInvalid
	}
	if err = v2input.VerifySemanticCalibration(pin.Calibration, calibrationRaw, pin.ModelLockSHA, pin.DatasetSHA); err != nil {
		return err
	}
	return ctx.Err()
}
