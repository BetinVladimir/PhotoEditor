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
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type NegativeQualificationCatalog interface {
	ResolveNegativeQualification(context.Context, auth.Identity, uuid.UUID, uuid.UUID, NegativeQualificationPin) (string, objectstore.Object, objectstore.Object, error)
}

// LoadNegativeQualificationEvidence resolves live authority, reads only the two
// retained object versions and verifies complete content before any admission.
// The catalog must finish its SQL transaction before returning locators.
func LoadNegativeQualificationEvidence(ctx context.Context, catalog NegativeQualificationCatalog, objects v2input.SessionAnalysisObjectReader,
	who auth.Identity, tenant, project uuid.UUID, pin NegativeQualificationPin, locks v2qualification.DeploymentLocks, params NegativeParams, inputICC string) (LocatedNegativeQualification, error) {
	var zero LocatedNegativeQualification
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := pin.Compile(); err != nil {
		return zero, err
	}
	if _, _, err := params.Compile(); err != nil {
		return zero, err
	}
	if params.Calibration != pin.Calibration {
		return zero, v2qualification.ErrUnqualified
	}
	if _, err := domain.ParseDigest(inputICC); err != nil {
		return zero, v2input.ErrInvalid
	}
	// Own policy arrays before invoking external callbacks.
	locks.RequiredChecks = append([]string(nil), locks.RequiredChecks...)
	locks.CalibrationSHAs = append([]string(nil), locks.CalibrationSHAs...)
	locks.TestFixtureSHAs = append([]string(nil), locks.TestFixtureSHAs...)
	locks.EvidenceSHAs = append([]string(nil), locks.EvidenceSHAs...)
	policySHA, err := NegativePolicySHA(pin.PolicyID, locks)
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
	bucket, report, calibration, err := catalog.ResolveNegativeQualification(ctx, who, tenant, project, pin)
	if err != nil {
		return zero, err
	}
	if bucket == "" || len(bucket) > 255 {
		return zero, v2input.ErrInvalid
	}
	for i, obj := range []objectstore.Object{report, calibration} {
		ref, max := pin.Report, int64(8<<20)
		if i == 1 {
			ref, max = pin.Calibration, 65536
		}
		if obj.SHA256 != ref.SHA256 || obj.Bytes < 2 || obj.Bytes > max || obj.ContentType != "application/json" || obj.Key == "" || len(obj.Key) > 1024 || len(obj.VersionID) > 1024 || obj.VersionID == "" || obj.VersionID == "null" {
			return zero, v2input.ErrInvalid
		}
	}
	checkpoint := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		currentBucket, currentReport, currentCalibration, err := catalog.ResolveNegativeQualification(ctx, who, tenant, project, pin)
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
			if _, err := VerifyNegativeCalibration(params, raw, inputICC); err != nil {
				return zero, err
			}
		}
		if err := checkpoint(); err != nil {
			return zero, err
		}
		bodies[i] = raw
	}
	verified, err := VerifyNegativeQualificationEvidence(pin, locks, bodies[0], bodies[1], params, inputICC)
	if err != nil {
		return zero, err
	}
	if err := checkpoint(); err != nil {
		return zero, err
	}
	return BindNegativeQualificationObjects(verified, bucket, report, calibration)
}
