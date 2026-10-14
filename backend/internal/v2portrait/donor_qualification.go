package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type DonorQualificationPin struct {
	PolicyID         uuid.UUID   `json:"policy_id"`
	QualificationID  uuid.UUID   `json:"qualification_id"`
	PolicySHA        string      `json:"policy_sha"`
	Report           v2input.Ref `json:"report"`
	Calibration      v2input.Ref `json:"calibration"`
	ModelLockSHA     string      `json:"model_lock_sha"`
	RuntimeLockSHA   string      `json:"runtime_lock_sha"`
	DeviceProfileSHA string      `json:"device_profile_sha"`
	DatasetSHA       string      `json:"dataset_sha"`
	IssuerID         uuid.UUID   `json:"issuer_id"`
	Backend          string      `json:"backend"`
}

func (p DonorQualificationPin) Compile() ([]byte, error) {
	if p.PolicyID == uuid.Nil || p.QualificationID == uuid.Nil || p.IssuerID == uuid.Nil || p.Backend != "cpu" || p.Report.ArtifactID == uuid.Nil || p.Calibration.ArtifactID == uuid.Nil || p.Report.ArtifactID == p.Calibration.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	for _, sha := range []string{p.PolicySHA, p.Report.SHA256, p.Calibration.SHA256, p.ModelLockSHA, p.RuntimeLockSHA, p.DeviceProfileSHA, p.DatasetSHA} {
		if _, err := domain.ParseDigest(sha); err != nil {
			return nil, v2input.ErrInvalid
		}
	}
	return canonical.Marshal(p)
}

// LockDonorQualification holds policy/report/calibration locks through the
// caller transaction. Exact evidence bytes must be verified independently;
// this relational gate alone neither qualifies runtime nor activates a task.
func LockDonorQualification(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, project uuid.UUID, pin DonorQualificationPin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	body, err := pin.Compile()
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
	if err := tx.QueryRow(ctx, "SELECT photo.lock_v28_donor_qualification($1,$2,$3,$4,$5::jsonb)", tenant, project, who.UserID, who.SessionID, string(body)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	return ctx.Err()
}
