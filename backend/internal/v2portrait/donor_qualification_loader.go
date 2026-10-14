package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type DonorQualificationCatalog interface {
	ResolveDonorQualification(context.Context, auth.Identity, uuid.UUID, uuid.UUID, DonorQualificationPin) (string, objectstore.Object, objectstore.Object, error)
}

// LoadDonorQualificationEvidence resolves live authority, reads only the two
// retained object versions and verifies complete content before any admission.
// The catalog must finish its SQL transaction before returning locators.
func LoadDonorQualificationEvidence(ctx context.Context, catalog DonorQualificationCatalog, objects v2input.SessionAnalysisObjectReader,
	who auth.Identity, tenant, project uuid.UUID, pin DonorQualificationPin, locks v2qualification.DeploymentLocks) (LocatedDonorQualification, error) {
	var zero LocatedDonorQualification
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := pin.Compile(); err != nil {
		return zero, err
	}
	// Own policy arrays before invoking external callbacks.
	locks.RequiredChecks = append([]string(nil), locks.RequiredChecks...)
	locks.CalibrationSHAs = append([]string(nil), locks.CalibrationSHAs...)
	locks.TestFixtureSHAs = append([]string(nil), locks.TestFixtureSHAs...)
	locks.EvidenceSHAs = append([]string(nil), locks.EvidenceSHAs...)
	policySHA, err := DonorPolicySHA(pin.PolicyID, locks)
	if err != nil {
		return zero, err
	}
	if policySHA != pin.PolicySHA {
		return zero, v2qualification.ErrUnqualified
	}
	bucket, report, calibration, err := catalog.ResolveDonorQualification(ctx, who, tenant, project, pin)
	if err != nil {
		return zero, err
	}
	for i, obj := range []objectstore.Object{report, calibration} {
		ref, max := pin.Report, int64(8<<20)
		if i == 1 {
			ref, max = pin.Calibration, 1<<20
		}
		if obj.SHA256 != ref.SHA256 || obj.Bytes < 2 || obj.Bytes > max || obj.ContentType != "application/json" || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
			return zero, v2input.ErrInvalid
		}
	}
	checkpoint := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		currentBucket, currentReport, currentCalibration, err := catalog.ResolveDonorQualification(ctx, who, tenant, project, pin)
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
		if err := checkpoint(); err != nil {
			return zero, err
		}
		bodies[i] = raw
	}
	verified, err := VerifyDonorQualificationEvidence(pin, locks, bodies[0], bodies[1])
	if err != nil {
		return zero, err
	}
	if err := checkpoint(); err != nil {
		return zero, err
	}
	return BindDonorQualificationObjects(verified, bucket, report, calibration)
}
