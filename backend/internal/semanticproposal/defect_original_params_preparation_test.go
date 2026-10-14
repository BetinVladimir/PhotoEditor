package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestOriginDefectParamsRefusesAbsentProvenanceBeforeSQL(t *testing.T) {
	reader := SQLTransferSourceReader{DB: &pgxpool.Pool{}}
	got, err := reader.prepareOriginDefectParams(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), v2input.SessionVersionBinding{}, TransferColorProfile{}, defectOriginPreparationProof{})
	if !errors.Is(err, v2input.ErrInvalid) || got != (defectManualParamsPreparation{}) {
		t.Fatal("unproven origin entered preparation", err)
	}
}

func TestOriginDefectParamsCancellationPrecedesIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{DB: &pgxpool.Pool{}}
	got, err := reader.prepareOriginDefectParams(ctx, auth.Identity{}, uuid.Nil, v2input.SessionVersionBinding{}, TransferColorProfile{}, defectOriginPreparationProof{})
	if !errors.Is(err, context.Canceled) || got != (defectManualParamsPreparation{}) {
		t.Fatal("cancelled preparation entered IO", err)
	}
}
