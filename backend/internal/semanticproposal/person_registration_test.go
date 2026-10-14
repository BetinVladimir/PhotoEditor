package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestPersonRegistrationRejectsMismatchedShellBeforeSQL(t *testing.T) {
	id := uuid.New()
	request := Request{TenantID: uuid.New(), RunID: uuid.New()}
	base := v2input.DraftTask{ID: id, Operation: "mask.person", OperationVersion: "1", ResourceClass: "gpu"}
	for _, change := range []func(*v2input.DraftTask){
		func(d *v2input.DraftTask) { d.ID = uuid.New() },
		func(d *v2input.DraftTask) { d.Operation = "face.parse" },
		func(d *v2input.DraftTask) { d.OperationVersion = "2" },
		func(d *v2input.DraftTask) { d.ResourceClass = "cpu" },
	} {
		draft := base
		change(&draft)
		// An embedded nil pgx.Tx panics if SQL is reached.
		state, err := personPreparationRegistrationState(context.Background(), &inertAdmissionTx{}, request, draft, v2input.GraphTask{ID: id})
		if state != "" || !errors.Is(err, v2input.ErrInvalid) {
			t.Fatalf("mismatched shell accepted: %q %v", state, err)
		}
	}
}

func TestPersonRegistrationCancellationPrecedesValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := personPreparationRegistrationState(ctx, nil, Request{}, v2input.DraftTask{}, v2input.GraphTask{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
