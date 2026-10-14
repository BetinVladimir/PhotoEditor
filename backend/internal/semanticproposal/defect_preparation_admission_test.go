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

type refusedDefectPreparationPublisher struct{ calls int }

func (p *refusedDefectPreparationPublisher) Publish(context.Context, uuid.UUID, int, string, []byte) (v2input.Ref, error) {
	p.calls++
	return v2input.Ref{}, errors.New("unexpected publication")
}

func TestPendingDefectAdmissionRejectsMissingAuthorityBeforeIO(t *testing.T) {
	publisher := &refusedDefectPreparationPublisher{}
	// An uninitialized pool would panic if admission reached SQL. The absent
	// private person/source authority must be rejected before either IO path.
	ticket, err := (SQLTransferSourceReader{}).admitPendingDefectPreparation(context.Background(), &pgxpool.Pool{}, publisher, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, defectPreparationDraftPlan{}, personProtectionProof{})
	if !errors.Is(err, v2input.ErrInvalid) || ticket != (v2input.JobTicket{}) || publisher.calls != 0 {
		t.Fatal("missing authority escaped preflight", ticket, err, publisher.calls)
	}
}

func TestPendingDefectAdmissionCancellationPrecedesDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ticket, err := (SQLTransferSourceReader{}).admitPendingDefectPreparation(ctx, nil, nil, auth.Identity{}, defectPreparationDraftPlan{}, personProtectionProof{})
	if !errors.Is(err, context.Canceled) || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled admission escaped", ticket, err)
	}
}
