package v2optical

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

type PerspectiveQualificationPin struct {
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

func (p PerspectiveQualificationPin) Compile() ([]byte, error) {
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
