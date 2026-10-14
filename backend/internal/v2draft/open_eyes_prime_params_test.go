package v2draft

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2portrait"
)

func TestOpenEyesPrimeParamsRejectsMissingRecoveryAuthority(t *testing.T) {
	deps := OpenEyesRecoveryDependencies{Project: uuid.New(), Base: uuid.New(), Donor: uuid.New()}
	params := v2portrait.OpenEyesParams{}
	params.Base.AssetVersionID = deps.Base
	params.Donor.AssetVersionID = deps.Donor
	if err := deps.PrimeParamsIntent(context.Background(),
		auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(),
		deps.Project, uuid.New(), uuid.New(), 0, params,
		v2portrait.DonorQualificationPin{}, DonorManualProducer{}); err == nil {
		t.Fatal("unconfigured recovery published Params")
	}
}
