package v2render

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestAuthoredFaceAdmissionPreservesCancellationBeforeDraftOrPublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := AuthoredFacePreparation{}
	calls := []struct {
		name string
		run  func() error
	}{
		{"detector", func() error {
			return p.AdmitDetector(ctx, auth.Identity{}, AuthoredInstructionSource{}, nil, v2qualification.Expected{}, v2input.Ref{}, .9, 64, AuthoredFaceBudget{})
		}},
		{"landmarks", func() error {
			return p.AdmitLandmarks(ctx, auth.Identity{}, AuthoredInstructionSource{}, nil, v2qualification.Expected{}, v2input.Ref{}, AuthoredFaceBudget{})
		}},
		{"parser", func() error {
			return p.AdmitParser(ctx, auth.Identity{}, AuthoredInstructionSource{}, nil, v2qualification.Expected{}, v2input.Ref{}, AuthoredFaceBudget{})
		}},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			if err := call.run(); !errors.Is(err, context.Canceled) {
				t.Fatalf("lost original cancellation: %v", err)
			}
		})
	}
}
