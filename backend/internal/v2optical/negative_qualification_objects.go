package v2optical

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type LocatedNegativeQualification struct {
	verified VerifiedNegativeQualification
	objects  [2]NegativeQualificationObject
}

// Objects returns owned original metadata for backend durable recovery.
func (v LocatedNegativeQualification) Objects() ([]NegativeQualificationObject, error) {
	if _, err := v.Qualification(); err != nil {
		return nil, err
	}
	return append([]NegativeQualificationObject(nil), v.objects[:]...), nil
}

// BindNegativeQualificationObjects joins exact verified bytes with authorized
// catalog locations. The object reader must have read these original versions;
// a caller supplying metadata without catalog authority cannot establish scope.
func BindNegativeQualificationObjects(verified VerifiedNegativeQualification, bucket string, report, calibration objectstore.Object) (LocatedNegativeQualification, error) {
	var zero LocatedNegativeQualification
	pin, err := verified.Pin()
	if err != nil {
		return zero, err
	}
	if bucket == "" || len(bucket) > 255 {
		return zero, v2input.ErrInvalid
	}
	refs := []v2input.Ref{pin.Calibration, pin.Report}
	objs := []objectstore.Object{calibration, report}
	bodies := [][]byte{verified.calibrationRaw, verified.reportRaw}
	labels := []string{"NegativeCalibration", "QualificationReport"}
	value := LocatedNegativeQualification{verified: verified}
	for i, obj := range objs {
		if obj.SHA256 != refs[i].SHA256 || obj.Bytes != int64(len(bodies[i])) || obj.ContentType != "application/json" || obj.Key == "" || len(obj.Key) > 1024 || obj.VersionID == "" || obj.VersionID == "null" || len(obj.VersionID) > 1024 {
			return zero, v2input.ErrInvalid
		}
		value.objects[i] = NegativeQualificationObject{Contract: labels[i], ArtifactID: refs[i].ArtifactID, SHA256: refs[i].SHA256, Bucket: bucket, Key: obj.Key, Version: obj.VersionID, Bytes: obj.Bytes}
	}
	return value, nil
}

func (v LocatedNegativeQualification) Qualification() (VerifiedNegativeQualification, error) {
	if err := v.verified.Verify(); err != nil {
		return VerifiedNegativeQualification{}, err
	}
	if v.objects[0].ArtifactID != v.verified.pin.Calibration.ArtifactID || v.objects[1].ArtifactID != v.verified.pin.Report.ArtifactID {
		return VerifiedNegativeQualification{}, v2input.ErrInvalid
	}
	return v.verified, nil
}

// LockLocatedNegativeQualification retains policy and exact locator locks until
// caller transaction completion. It performs no object IO or commit.
func LockLocatedNegativeQualification(ctx context.Context, tx pgx.Tx, who auth.Identity,
	tenant, project uuid.UUID, value LocatedNegativeQualification) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	verified, err := value.Qualification()
	if err != nil {
		return err
	}
	if err := LockNegativeQualification(ctx, tx, who, tenant, project, verified.pin); err != nil {
		return err
	}
	body, err := verified.pin.Compile()
	if err != nil {
		return err
	}
	objects, err := canonical.Marshal(value.objects)
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_v29_negative_qualification_objects($1,$2,$3,$4,$5::jsonb,$6::jsonb)",
		tenant, project, who.UserID, who.SessionID, string(body), string(objects)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
