package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestCompositionStageRejectsInvalidShellBeforeIO(t *testing.T) {
	run, session := uuid.New(), uuid.New()
	workflow, _ := v2input.WorkflowID(run)
	requests := compositionDraftRequests(t, run, session)
	draft, _, err := CompileCompositionDraft(workflow, 0, requests[0].Preparation, requests[0].Resolved)
	if err != nil {
		t.Fatal(err)
	}
	stage := SQLOriginalCompositionTaskStage{DB: &pgxpool.Pool{}}
	changes := []func(*v2input.DraftTask){
		func(d *v2input.DraftTask) { d.ID = uuid.New() },
		func(d *v2input.DraftTask) { d.NodeKey = "layer.compose:1" },
		func(d *v2input.DraftTask) { d.Operation = "optical.lens" },
		func(d *v2input.DraftTask) { d.ResourceClass = "gpu" },
		func(d *v2input.DraftTask) { d.OperationVersion = "2" },
		func(d *v2input.DraftTask) { d.ParamsArtifactID = uuid.Nil },
		func(d *v2input.DraftTask) { d.InputDigest = "invalid" },
		func(d *v2input.DraftTask) {
			d.ExecutionProfile = append(append([]byte(nil), d.ExecutionProfile...), ' ')
		},
	}
	for _, change := range changes {
		wrong := draft
		change(&wrong)
		if err := stage.Stage(context.Background(), requests[0].Preparation.Tenant, run, workflow, 0, wrong); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := stage.Stage(ctx, requests[0].Preparation.Tenant, run, workflow, 0, draft); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
