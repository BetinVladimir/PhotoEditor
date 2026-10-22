package v2render

import (
	"context"
	"errors"
	"photoeditor/backend/internal/v2input"
	"testing"
)

// Real entrypoints must return cancellation before any SQL/publisher dependency.
func TestAuthoredMaskTransformAdmissionCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := AuthoredMaskTransformPreparation{}
	if _, err := p.Prepare(ctx, AuthoredInstructionSource{}, AuthoredMaskTransformDescriptor{}, nil, nil, 0, 0, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := p.Admit(ctx, AuthoredInstructionSource{}, AuthoredMaskTransformPlan{}, nil, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := (AuthoredMaskTransformParamsPublication{}).Publish(ctx, AuthoredInstructionSource{}, AuthoredMaskTransformPlan{}, v2input.Ref{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
