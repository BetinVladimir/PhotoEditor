package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// OpenEyesConfirmationService is the server issuance composition. DB must use
// scheduler credentials; object IO is separated from every database transaction.
// The HTTP route must obtain tenant/project/run/session and observations through
// authenticated request handling, not expose server person/epoch/actor fields.
type OpenEyesConfirmationService struct {
	DB      DonorConfirmationTransactions
	Objects artifactjson.Objects
	Bucket  string
}

func (s OpenEyesConfirmationService) Issue(ctx context.Context, who auth.Identity,
	tenant, project, shoot, run uuid.UUID, params OpenEyesParams, sourceObservation, donorObservation uuid.UUID) (OpenEyesParams, error) {
	if s.DB == nil || s.Objects == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return OpenEyesParams{}, v2input.ErrInvalid
	}
	repository := SQLDonorConfirmationRepository{DB: s.DB, Bucket: s.Bucket}
	publisher := DonorConfirmationObjectPublisher{Repository: repository, Objects: s.Objects}
	return IssueOpenEyesConfirmation(ctx, s.DB, publisher, who, tenant, project, shoot, run, params, sourceObservation, donorObservation)
}
