package v2learning

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestEvaluateDefectAuthorityRefusesUninstalledAndForeignRefs(t *testing.T) {
	lease := LearningEvaluateLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Hour), TenantID: uuid.New(),
		RunID: uuid.New(), WorkflowID: uuid.New(), Candidate: v2input.Ref{
			ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}}
	event := uuid.New()
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	caseRef := TrainingRenderCase{DefectEvidence: &ref}
	reader := SQLFrozenEvaluateDefectReader{Bucket: "photo"}
	if _, err := reader.Load(context.Background(), lease, event, caseRef); !errors.Is(err, ErrInvalidTrainingDraft) {
		t.Fatalf("uninstalled reader accepted evidence: %v", err)
	}
	if _, err := reader.locate(context.Background(), lease, event, caseRef); !errors.Is(err, ErrInvalidTrainingDraft) {
		t.Fatalf("missing database accepted foreign ref: %v", err)
	}
}
