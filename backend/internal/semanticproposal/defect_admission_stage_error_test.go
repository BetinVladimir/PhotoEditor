package semanticproposal

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDefectAdmissionStageErrorPreservesEveryIdentity(t *testing.T) {
	stages := []defectAdmissionStage{defectStageRegisteredContext, defectStageRegisteredTransaction, defectStageRegistrationPacket, defectStageAdmissionPacket, defectStagePersonAuthority, defectStageRegistrationQuery, defectStageRegistrationState, defectStageRegisteredPolicy, defectStageEvidenceQuery, defectStageEvidenceState, defectStageRegisteredCompletion, defectStagePersonTransaction, defectStagePersonValidation, defectStageBeginDraft, defectStagePersonProofs, defectStagePersonPacket, defectStageActivationContent, defectStagePersonQuery, defectStagePersonState, defectStagePersonPolicy}
	causes := []error{v2input.ErrConflict, v2input.ErrInvalid, context.Canceled, context.DeadlineExceeded, &pgconn.PgError{Code: "42501", Message: "denied"}}
	for _, stage := range stages {
		for _, cause := range causes {
			wrapped := defectAdmissionStageError(stage, cause)
			if !errors.Is(wrapped, cause) || errors.Unwrap(wrapped) != cause {
				t.Fatalf("identity lost at %s", stage)
			}
			var before, after *pgconn.PgError
			if errors.As(cause, &before) && (!errors.As(wrapped, &after) || after != before) {
				t.Fatalf("SQL identity lost at %s", stage)
			}
			if defectAdmissionStageError(stage, nil) != nil {
				t.Fatalf("nil became failure at %s", stage)
			}
		}
	}
}
