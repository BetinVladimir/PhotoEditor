package interchange

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func TestSQLImportActivationRejectsMissingPrivateQualification(t *testing.T) {
	if err := (SQLImportActivator{Tenant: uuid.New()}).ActivateImport(context.Background(), QualifiedImportActivation{}); err == nil {
		t.Fatal("missing qualification activated")
	}
}
func TestSQLImportActivationCancellationBeforeDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (SQLImportActivator{}).ActivateImport(ctx, QualifiedImportActivation{}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}

func TestImportReviewActivationGrantRaceBecomesAwaitingDecision(t *testing.T) {
	stale := fmt.Errorf("transaction rolled back: %w", &pgconn.PgError{Code: "P0001", Message: "V30_IMPORT_REVIEW_ACTIVATION_GRANT"})
	if !errors.Is(classifyImportReviewActivationError(stale), ErrImportReviewAwaitingDecision) {
		t.Fatal("revoked grant was treated as failed import")
	}
	for _, original := range []error{
		&pgconn.PgError{Code: "P0001", Message: "V30_IMPORT_REVIEW_ACTIVATION_PROPOSAL"},
		&pgconn.PgError{Code: "23503", Message: "V30_IMPORT_REVIEW_ACTIVATION_GRANT"},
		errors.New("V30_IMPORT_REVIEW_ACTIVATION_GRANT"),
	} {
		if classifyImportReviewActivationError(original) != original {
			t.Fatal("unrelated activation error was hidden", original)
		}
	}
}
