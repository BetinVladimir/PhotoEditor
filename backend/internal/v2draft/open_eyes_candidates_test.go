package v2draft

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
)

func TestOpenEyesCandidatesRequireInstalledProject(t *testing.T) {
	var service *OpenEyesSubmissionService
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if _, err := service.ListProjectSourceCandidates(context.Background(), who,
		uuid.New(), uuid.New(), GlareSourceCandidateQuery{}); err == nil {
		t.Fatal("uninstalled source catalog accepted")
	}
	if _, err := service.ListProjectCandidates(context.Background(), who,
		uuid.New(), uuid.New(), GlareDonorCandidateQuery{}); err == nil {
		t.Fatal("uninstalled donor catalog accepted")
	}
	if _, err := service.ListProjectMaskPairs(context.Background(), who,
		uuid.New(), uuid.New(), GlareMaskPairQuery{}); err == nil {
		t.Fatal("uninstalled mask catalog accepted")
	}
}
