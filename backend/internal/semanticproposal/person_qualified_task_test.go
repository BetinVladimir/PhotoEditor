package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestQualifiedPersonRejectsWrongDeploymentBeforeIO(t *testing.T) {
	s := SQLTransferSourceReader{}
	cases := []v2qualification.Expected{
		{Operation: "mask.person", Version: "1", Backend: "cpu"},
		{Operation: "face.parse", Version: "1", Backend: "cuda"},
		{Operation: "mask.person", Version: "2", Backend: "cuda"},
	}
	for _, expected := range cases {
		frozen, err := s.prepareQualifiedCatalogPersonTask(context.Background(), nil, auth.Identity{}, uuid.Nil, uuid.Nil, 0, v2input.SessionVersionBinding{}, nil, v2input.Ref{}, v2input.Ref{}, expected, 512, v2input.SemanticQualifiedCosts{}, 0, 0, 0, 0)
		if !errors.Is(err, v2qualification.ErrUnqualified) {
			t.Fatal("wrong deployment not refused before IO", err)
		}
		if _, err := frozen.frozen.DraftTask(); err == nil {
			t.Fatal("partial task escaped")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.prepareQualifiedCatalogPersonTask(ctx, nil, auth.Identity{}, uuid.Nil, uuid.Nil, 0, v2input.SessionVersionBinding{}, nil, v2input.Ref{}, v2input.Ref{}, v2qualification.Expected{}, 512, v2input.SemanticQualifiedCosts{}, 0, 0, 0, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
