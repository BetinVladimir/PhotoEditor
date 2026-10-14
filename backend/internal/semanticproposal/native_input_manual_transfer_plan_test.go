package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputManualTransferCompilerRejectsCancelledAndEmptyBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	plan, err := reader.compileNativeInputManualTransferWorkflowPlan(ctx, auth.Identity{}, Request{}, PublishedIntent{}, v2input.Ref{}, nil, nil, nil, v2input.SemanticQualificationPin{}, nil, TransferPreparation{})
	if !errors.Is(err, context.Canceled) || len(plan.core.tasks) != 0 || plan.reference.reference != nil {
		t.Fatalf("cancelled compiler exposed plan: %v", err)
	}
	plan, err = reader.compileNativeInputManualTransferWorkflowPlan(context.Background(), auth.Identity{}, Request{}, PublishedIntent{}, v2input.Ref{}, nil, nil, nil, v2input.SemanticQualificationPin{}, nil, TransferPreparation{})
	if !errors.Is(err, v2input.ErrInvalid) || len(plan.core.tasks) != 0 {
		t.Fatalf("empty compiler exposed plan: %v", err)
	}
}
