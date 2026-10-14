package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeSourcePreparationPreflightReturnsNoPartialAuthority(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := Request{TenantID: uuid.New(), ActorID: who.UserID}
	reader := SQLTransferSourceReader{ProjectID: uuid.New()}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, mode := range []string{"cancelled", "foreign-actor", "missing-session", "missing-project", "missing-task"} {
		ctx := context.Background()
		identity := who
		scoped := reader
		task := uuid.New()
		switch mode {
		case "cancelled":
			ctx = cancelled
		case "foreign-actor":
			identity.UserID = uuid.New()
		case "missing-session":
			identity.SessionID = uuid.Nil
		case "missing-project":
			scoped.ProjectID = uuid.Nil
		case "missing-task":
			task = uuid.Nil
		}
		metadata, proof, err := scoped.prepareNativeSourceContinuation(ctx, identity, request, "key", v2input.Ref{}, v2input.Ref{}, task, v2input.Ref{}, nativeDefectQualificationReports{}, TransferPersonProtection{}, TransferColorProfile{})
		if err == nil || metadata.Width != 0 || metadata.Height != 0 || proof.source.pins != "" || proof.native.defect.verified || len(proof.native.defect.faces) != 0 {
			t.Fatal("partial native source authority", mode, err)
		}
		if mode == "cancelled" && !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost", err)
		}
	}
}
