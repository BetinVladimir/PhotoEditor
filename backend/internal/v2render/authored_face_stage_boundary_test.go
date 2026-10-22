package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestAuthoredFaceStageCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := AuthoredFacePreparation{}
	_, err := p.PrepareDetector(ctx, auth.Identity{}, AuthoredInstructionSource{}, nil, v2qualification.Expected{}, v2input.Ref{}, .9, 64, AuthoredFaceBudget{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("detector cancellation identity: %v", err)
	}
	_, err = p.PrepareLandmarks(ctx, auth.Identity{}, AuthoredInstructionSource{}, nil, v2qualification.Expected{}, v2input.Ref{}, AuthoredFaceBudget{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("landmark cancellation identity: %v", err)
	}
	_, err = p.PrepareParser(ctx, auth.Identity{}, AuthoredInstructionSource{}, nil, v2qualification.Expected{}, v2input.Ref{}, AuthoredFaceBudget{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("parser cancellation identity: %v", err)
	}
}
func TestAuthoredFaceTransportRejectsNonTransportPurposeBeforeSQL(t *testing.T) {
	r := authoredFaceTransportRepository{base: authoredFaceParamsRepository{scope: artifactjson.Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New()}}}
	for _, purpose := range []string{"task_params", "v2_input_chunk_00", "v2_input_chunk_-1", "v2_input_chunk_1954", "v2_input_chunk_0x1", "v2_input_manifest_extra", "v2_input_chunk_1/other"} {
		t.Run(purpose, func(t *testing.T) {
			scope := r.base.scope
			scope.Purpose = purpose
			if err := r.check(context.Background(), nil, scope); err == nil {
				t.Fatal("non-transport scope reached SQL")
			}
		})
	}
}
