package v2draft

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2portrait"
	"photoeditor/backend/internal/v2qualification"
)

func TestOpenEyesSubmissionRequiresInstalledProjectBeforeIO(t *testing.T) {
	if service, err := NewOpenEyesSubmissionService(nil, nil, nil, "photos",
		uuid.New(), uuid.New(), v2portrait.DonorQualificationPin{},
		v2qualification.DeploymentLocks{}, 100, 100,
		v2portrait.DonorResourceEstimate{OutputBytes: 1}, minRestorationDraftReservation+1); err == nil || service != nil {
		t.Fatal("uninstalled open eyes service accepted")
	}
	var service *OpenEyesSubmissionService
	if ticket, err := service.SubmitProjectKeyed(context.Background(),
		auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(),
		uuid.New(), "eyes-request", OpenEyesClientIntent{}); err == nil ||
		ticket.RunID != uuid.Nil {
		t.Fatal("uninstalled submit returned a ticket")
	}
}
