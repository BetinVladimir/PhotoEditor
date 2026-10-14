package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type DonorQualificationObject struct {
	Contract   string    `json:"contract"`
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"object_key"`
	Version    string    `json:"object_version"`
	Bytes      int64     `json:"bytes"`
}

type LocatedDonorQualification struct {
	verified VerifiedDonorQualification
	objects  [2]DonorQualificationObject
}

// Objects returns owned original metadata for backend durable recovery.
func (v LocatedDonorQualification) Objects() ([]DonorQualificationObject, error) {
	if _, err := v.Qualification(); err != nil {
		return nil, err
	}
	return append([]DonorQualificationObject(nil), v.objects[:]...), nil
}

// BindDonorQualificationObjects joins exact verified bytes with authorized
// catalog locations. The object reader must have read these original versions;
// a caller supplying metadata without catalog authority cannot establish scope.
func BindDonorQualificationObjects(verified VerifiedDonorQualification, bucket string, report, calibration objectstore.Object) (LocatedDonorQualification, error) {
	var zero LocatedDonorQualification
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
	labels := []string{"Calibration", "Report"}
	value := LocatedDonorQualification{verified: verified}
	for i, obj := range objs {
		if obj.SHA256 != refs[i].SHA256 || obj.Bytes != int64(len(bodies[i])) || obj.ContentType != "application/json" || obj.Key == "" || len(obj.Key) > 1024 || obj.VersionID == "" || obj.VersionID == "null" || len(obj.VersionID) > 1024 {
			return zero, v2input.ErrInvalid
		}
		value.objects[i] = DonorQualificationObject{Contract: labels[i], ArtifactID: refs[i].ArtifactID, SHA256: refs[i].SHA256, Bucket: bucket, Key: obj.Key, Version: obj.VersionID, Bytes: obj.Bytes}
	}
	return value, nil
}

func (v LocatedDonorQualification) Qualification() (VerifiedDonorQualification, error) {
	if err := v.verified.Verify(); err != nil {
		return VerifiedDonorQualification{}, err
	}
	if v.objects[0].ArtifactID != v.verified.pin.Calibration.ArtifactID || v.objects[1].ArtifactID != v.verified.pin.Report.ArtifactID {
		return VerifiedDonorQualification{}, v2input.ErrInvalid
	}
	return v.verified, nil
}

// LockLocatedDonorQualification repeats current policy/evidence locks and checks
// retained bucket/key/version/SHA/bytes equality. Locks last until caller commit.
func LockLocatedDonorQualification(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, project uuid.UUID, value LocatedDonorQualification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	verified, err := value.Qualification()
	if err != nil {
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
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_v28_donor_qualification_objects($1,$2,$3,$4,$5::jsonb,$6::jsonb)", tenant, project, who.UserID, who.SessionID, string(body), string(objects)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
