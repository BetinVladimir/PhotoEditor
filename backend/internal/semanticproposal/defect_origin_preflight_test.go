package semanticproposal

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestOriginDefectExecutionImpossibleBudgetPrecedesEvidenceIO(t *testing.T) {
	// An uninitialized pool panics if reached. A budget below the worker's
	// fixed ROI overhead must refuse before source/catalog/object processing.
	reader := SQLTransferSourceReader{DB: &pgxpool.Pool{}, ProjectID: uuid.New(), Bucket: "test"}
	got, err := reader.prepareOriginSelectedDefectExecution(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), v2input.SessionVersionBinding{}, TransferColorProfile{}, DefectOriginPins{}, v2input.SemanticQualifiedCosts{}, 1<<30, 0, 1<<40, 1<<40)
	if !errors.Is(err, v2input.ErrInvalid) || !reflect.DeepEqual(got, defectPolicyExecutionPreparation{}) {
		t.Fatal("impossible budget entered evidence IO", err)
	}
}

func TestOriginDefectExecutionCancellationPrecedesBudgetAndEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{DB: &pgxpool.Pool{}}
	got, err := reader.prepareOriginSelectedDefectExecution(ctx, auth.Identity{}, uuid.Nil, v2input.SessionVersionBinding{}, TransferColorProfile{}, DefectOriginPins{}, v2input.SemanticQualifiedCosts{}, 0, 0, 0, 0)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, defectPolicyExecutionPreparation{}) {
		t.Fatal("cancelled request entered evidence IO", err)
	}
}
