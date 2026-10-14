package v2draft

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2portrait"
	"photoeditor/backend/internal/v2qualification"
)

func TestOpenEyesRecoveryRefusesIncompleteInstallationBeforeIO(t *testing.T) {
	if _, err := NewSQLOpenEyesRecoveryDependencies(nil, nil, nil, "photos",
		uuid.New(), uuid.New(), uuid.New(), v2qualification.DeploymentLocks{},
		100, 100, v2portrait.DonorResourceEstimate{OutputBytes: 1}); err == nil {
		t.Fatal("uninstalled recovery accepted")
	}
	if got, err := (OpenEyesRecoveryDependencies{}).LoadManualProducer(context.Background(),
		auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(),
		OpenEyesClientIntent{}, v2portrait.OpenEyesParams{}); err == nil ||
		got.RootRaw != nil || got.ParamsRaw != nil {
		t.Fatal("uninstalled manual evidence loader reached IO")
	}
}
