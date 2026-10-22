package v2render

import (
	"context"
	"errors"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAuthoredGeometryAdmissionCancellationBeforeDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := SQLAuthoredGeometryAdmission{}
	if e := s.Stage(ctx, AuthoredInstructionSource{}, AuthoredGeometryTaskPlan{}, AuthoredGeometryScopeWitnesses{}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if e := s.Seal(ctx, AuthoredInstructionSource{}, AuthoredGeometryTaskPlan{}, v2input.GraphTask{}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if e := s.Admit(ctx, AuthoredInstructionSource{}, AuthoredGeometryTaskPlan{}, AuthoredGeometryScopeWitnesses{}, "", nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
