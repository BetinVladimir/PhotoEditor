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

func TestDefectQualificationPreparationRejectsWithoutIO(t *testing.T) {
	reader := SQLTransferSourceReader{ProjectID: uuid.New(), Bucket: "bucket"}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	proof, err := reader.readDefectQualificationPreparation(ctx, who, tenant, v2input.Ref{}, v2qualification.Expected{})
	if !errors.Is(err, context.Canceled) || proof != (defectQualificationPreparationProof{}) {
		t.Fatal("cancelled proof retained", err)
	}
	for _, op := range []string{"mask.person", "face.detect", "semantic.transfer", ""} {
		proof, err = reader.readDefectQualificationPreparation(context.Background(), who, tenant, v2input.Ref{}, v2qualification.Expected{Operation: op, Version: "1"})
		if err != v2qualification.ErrUnqualified || proof != (defectQualificationPreparationProof{}) {
			t.Fatal("foreign operation accepted", op, err)
		}
	}
	proof, err = reader.readDefectQualificationPreparation(context.Background(), who, uuid.Nil, v2input.Ref{}, v2qualification.Expected{Operation: "defects.propose", Version: "1"})
	if err != v2input.ErrInvalid || proof != (defectQualificationPreparationProof{}) {
		t.Fatal("missing tenant accepted", err)
	}
}
