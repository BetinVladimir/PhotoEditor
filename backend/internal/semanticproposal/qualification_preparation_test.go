package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestTransferQualificationPreparationRejectsBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	proof, err := reader.readTransferQualificationPreparation(ctx, auth.Identity{}, uuid.Nil, v2input.SemanticQualificationPin{}, v2qualification.Expected{})
	if !errors.Is(err, context.Canceled) || proof != (transferQualificationPreparationProof{}) {
		t.Fatalf("cancelled preparation retained evidence: %v", err)
	}
	// A nil database/store makes any accidental IO observable as a panic or a
	// different error. Incomplete authenticated scopes must leave no proof.
	for _, scope := range []struct{ tenant, project, actor, session uuid.UUID }{
		{uuid.Nil, uuid.New(), uuid.New(), uuid.New()},
		{uuid.New(), uuid.Nil, uuid.New(), uuid.New()},
		{uuid.New(), uuid.New(), uuid.Nil, uuid.New()},
		{uuid.New(), uuid.New(), uuid.New(), uuid.Nil},
	} {
		reader.ProjectID = scope.project
		proof, err = reader.readTransferQualificationPreparation(context.Background(), auth.Identity{UserID: scope.actor, SessionID: scope.session}, scope.tenant, v2input.SemanticQualificationPin{}, v2qualification.Expected{})
		if !errors.Is(err, v2input.ErrInvalid) || proof != (transferQualificationPreparationProof{}) {
			t.Fatalf("incomplete scope retained evidence: %v", err)
		}
	}
}
