package v2optical

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type MultiQualificationCatalog interface {
	ResolveMultiQualification(context.Context, auth.Identity, uuid.UUID, uuid.UUID, MultiQualificationPin, string) (string, objectstore.Object, objectstore.Object, error)
}

// LoadMultiQualificationEvidence resolves live authority, reads only the two
// retained object versions and verifies complete content before any admission.
// The catalog must finish its SQL transaction before returning locators.
func LoadMultiQualificationEvidence(ctx context.Context, catalog MultiQualificationCatalog, objects v2input.SessionAnalysisObjectReader,
	who auth.Identity, tenant, project uuid.UUID, pin MultiQualificationPin, locks v2qualification.DeploymentLocks, operation string, paramsRaw []byte, originals [][]byte) (LocatedMultiQualification, error) {
	var zero LocatedMultiQualification
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if locks.Operation != operation || catalog == nil || objects == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := pin.Compile(); err != nil {
		return zero, err
	}
	paramsRaw = append([]byte(nil), paramsRaw...)
	owned := make([][]byte, len(originals))
	for i, raw := range originals {
		owned[i] = append([]byte(nil), raw...)
	}
	originals = owned
	if _, err := VerifyMultiWorkingImages(operation, paramsRaw, originals); err != nil {
		return zero, err
	}

	// Own policy arrays before invoking external callbacks.
	locks.RequiredChecks = append([]string(nil), locks.RequiredChecks...)
	locks.CalibrationSHAs = append([]string(nil), locks.CalibrationSHAs...)
	locks.TestFixtureSHAs = append([]string(nil), locks.TestFixtureSHAs...)
	locks.EvidenceSHAs = append([]string(nil), locks.EvidenceSHAs...)
	policySHA, err := MultiPolicySHA(pin.PolicyID, locks)
	if err != nil {
		return zero, err
	}
	if policySHA != pin.PolicySHA {
		return zero, v2qualification.ErrUnqualified
	}
	expected, err := locks.Expected()
	if err != nil {
		return zero, err
	}
	if expected.ModelLockSHA != pin.ModelLockSHA || expected.RuntimeLockSHA != pin.RuntimeLockSHA || expected.DeviceProfileSHA != pin.DeviceProfileSHA || expected.DatasetSHA != pin.DatasetSHA || expected.IssuerID != pin.IssuerID {
		return zero, v2qualification.ErrUnqualified
	}
	if _, ok := expected.CalibrationSHAs[pin.Calibration.SHA256]; !ok {
		return zero, v2qualification.ErrUnqualified
	}
	bucket, report, calibration, err := catalog.ResolveMultiQualification(ctx, who, tenant, project, pin, operation)
	if err != nil {
		return zero, err
	}
	if bucket == "" || len(bucket) > 255 {
		return zero, v2input.ErrInvalid
	}
	for i, obj := range []objectstore.Object{report, calibration} {
		ref, max := pin.Report, int64(8<<20)
		if i == 1 {
			ref, max = pin.Calibration, 1<<20
		}
		if obj.SHA256 != ref.SHA256 || obj.Bytes < 2 || obj.Bytes > max || obj.ContentType != "application/json" || obj.Key == "" || len(obj.Key) > 1024 || len(obj.VersionID) > 1024 || obj.VersionID == "" || obj.VersionID == "null" {
			return zero, v2input.ErrInvalid
		}
	}
	checkpoint := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		currentBucket, currentReport, currentCalibration, err := catalog.ResolveMultiQualification(ctx, who, tenant, project, pin, operation)
		if err != nil {
			return err
		}
		if currentBucket != bucket || currentReport != report || currentCalibration != calibration {
			return v2input.ErrExpired
		}
		return ctx.Err()
	}
	var bodies [2][]byte
	for i, obj := range []objectstore.Object{report, calibration} {
		if err := checkpoint(); err != nil {
			return zero, err
		}
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		reader, err := objects.Open(ctx, obj)
		if err != nil {
			return zero, err
		}
		if reader == nil {
			return zero, v2input.ErrInvalid
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, obj.Bytes+1))
		closeErr := reader.Close()
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if readErr != nil {
			return zero, readErr
		}
		if closeErr != nil {
			return zero, closeErr
		}
		if int64(len(raw)) != obj.Bytes {
			return zero, v2input.ErrInvalid
		}
		sum := sha256.Sum256(raw)
		normalized, e := canonical.Bytes(raw)
		if hex.EncodeToString(sum[:]) != obj.SHA256 || e != nil || !bytes.Equal(normalized, raw) {
			return zero, v2input.ErrInvalid
		}
		if i == 1 {
			if err := v2input.VerifySemanticCalibration(pin.Calibration, raw, pin.ModelLockSHA, pin.DatasetSHA); err != nil {
				return zero, err
			}
		}
		if err := checkpoint(); err != nil {
			return zero, err
		}
		bodies[i] = raw
	}
	verified, err := VerifyMultiQualificationEvidence(operation, pin, locks, bodies[0], bodies[1], paramsRaw, originals)
	if err != nil {
		return zero, err
	}
	if err := checkpoint(); err != nil {
		return zero, err
	}
	return BindMultiQualificationObjects(verified, bucket, report, calibration)
}
