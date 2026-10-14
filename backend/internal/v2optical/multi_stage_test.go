package v2optical

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestMultiStageRejectsForeignSessionBeforeDatabase(t *testing.T) {
	operation := "optical.focus_stack"
	pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
	qualified, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals)
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := MultiResources(operation, params, originals)
	if err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	draft, err := CompileMultiDraft(context.Background(), who, tenant, uuid.New(), uuid.New(), uuid.NewString(), operation, params, originals, qualified, capacity)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := draft.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	published := v2input.Ref{ArtifactID: snapshot.ParamsDocument, SHA256: snapshot.ParamsSHA}
	wrong := who
	wrong.SessionID = uuid.New()
	if err := StageMultiTask(context.Background(), multiParamsNoIO{}, "photos", wrong, tenant, draft, qualified, published); err != v2input.ErrInvalid {
		t.Fatalf("foreign session reached database: %v", err)
	}
	changed := published
	changed.ArtifactID = uuid.New()
	if err := StageMultiTask(context.Background(), multiParamsNoIO{}, "photos", who, tenant, draft, qualified, changed); err != v2input.ErrInvalid {
		t.Fatalf("foreign Params reached database: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := StageMultiTask(ctx, multiParamsNoIO{}, "photos", who, tenant, draft, qualified, published); err != context.Canceled {
		t.Fatal(err)
	}
}
