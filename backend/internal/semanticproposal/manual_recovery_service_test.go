package semanticproposal

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestManualParentResponseRefusesChildTicket(t *testing.T) {
	request := Request{RunID: uuid.New(), WorkflowID: uuid.New()}
	good := v2input.JobTicket{RunID: request.RunID, WorkflowID: request.WorkflowID, State: "queued"}
	response, err := committedManualParentResponse(request, good)
	if err != nil || response.Status != 202 || response.ResourceID == nil || *response.ResourceID != request.RunID {
		t.Fatal(response, err)
	}
	for _, mode := range []string{"child", "workflow", "uncommitted"} {
		changed := good
		switch mode {
		case "child":
			changed.RunID = uuid.New()
		case "workflow":
			changed.WorkflowID = uuid.New()
		case "uncommitted":
			changed.State = "blocked"
		}
		out, err := committedManualParentResponse(request, changed)
		if err == nil || out.Body != nil || out.ResourceID != nil {
			t.Fatal("invalid parent ticket exposed response", mode, err)
		}
	}
}

func TestManualRecoveryServiceRefusesCancelledAndAnonymous(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := (ManualRecoveryService{}).Propose(current, auth.Identity{}, uuid.Nil, "", v2input.SemanticEditIntent{})
		if err == nil || out.Body != nil || out.ResourceID != nil {
			t.Fatal("incomplete service exposed response", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}
