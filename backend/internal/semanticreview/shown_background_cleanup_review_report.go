package semanticreview

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
)

// VerifyShownBackgroundCleanupReviewReport reads bounded exact-version bytes outside SQL
// transactions and repeats authenticated policy/object scope after IO. It proves
// report consistency with the imported policy, not the truth of measurements.
func VerifyShownBackgroundCleanupReviewReport(ctx context.Context, db ApprovedProtectionQueryer, read PlanObjectReader, bucket string,
	tenant, intent, actor, session uuid.UUID, revision int64, pin ExposureQualificationPin) error {
	if db == nil || read == nil || !validExposureQualification(pin) || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || revision < 1 {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	pinRaw, err := canonical.Marshal(pin)
	if err != nil {
		return ErrInvalid
	}
	load := func() ([]byte, exposureReportEvidence, error) {
		var raw json.RawMessage
		var evidence exposureReportEvidence
		if err := db.QueryRow(ctx, "SELECT photo.semantic_shown_background_cleanup_review_report_evidence($1,$2,$3,$4,$5,$6)", tenant, intent, actor, session, revision, pinRaw).Scan(&raw); err != nil {
			return nil, evidence, err
		}
		if len(raw) == 0 {
			return nil, evidence, ErrConflict
		}
		if len(raw) > 4<<20 {
			return nil, evidence, ErrInvalid
		}
		canonicalRaw, err := canonical.Bytes(raw)
		if err != nil {
			return nil, evidence, ErrInvalid
		}
		decoder := json.NewDecoder(bytes.NewReader(canonicalRaw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&evidence); err != nil {
			return nil, evidence, ErrInvalid
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return nil, evidence, ErrInvalid
		}
		return canonicalRaw, evidence, nil
	}
	before, evidence, err := load()
	if err != nil {
		return err
	}
	p := evidence.Policy
	if evidence.Pin != pin || p.SchemaVersion != 1 || p.PolicyID != pin.PolicyID || p.Operation != "semantic.adopt_shown_background_cleanup" || p.Version != "1" || p.Backend != "cpu" || p.Model != pin.ModelLockSHA || p.Runtime != pin.RuntimeLockSHA || p.Device != pin.DeviceProfileSHA || p.Dataset != pin.DatasetSHA || p.Issuer != pin.IssuerID || evidence.Report.ID != pin.Report.ArtifactID || evidence.Report.SHA != pin.Report.SHA256 || evidence.Report.Bucket != bucket || evidence.Report.Bytes < 2 || evidence.Report.Bytes > 8<<20 || evidence.Report.Media != "application/json" || evidence.Report.Key == "" || evidence.Report.Version == "" || evidence.Report.Version == "null" {
		return ErrInvalid
	}
	expected, err := (v2qualification.DeploymentLocks{Operation: p.Operation, Version: p.Version, Backend: p.Backend, ModelLockSHA: p.Model, RuntimeLockSHA: p.Runtime, DeviceProfileSHA: p.Device, DatasetSHA: p.Dataset, IssuerID: p.Issuer.String(), RequiredChecks: p.Checks, TestFixtureSHAs: p.Fixtures, CalibrationSHAs: p.Calibrations, EvidenceSHAs: p.Evidence, LicenseCleared: p.License}).Expected()
	if err != nil {
		return err
	}
	body, err := read(ctx, PlanSource{Ref: pin.Report, Bucket: bucket, Key: evidence.Report.Key, Version: evidence.Report.Version, Bytes: evidence.Report.Bytes, MediaType: evidence.Report.Media})
	if err != nil {
		return err
	}
	if _, err = v2qualification.Verify(body, pin.Report.SHA256, expected); err != nil {
		return err
	}
	after, _, err := load()
	if err != nil {
		return err
	}
	if !bytes.Equal(before, after) {
		return ErrConflict
	}
	return ctx.Err()
}
