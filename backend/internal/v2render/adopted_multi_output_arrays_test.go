package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestMultiOutputArrayPreflightRefusesUnverifiedDocuments(t *testing.T) {
	if _, err := multiOutputArrays(MultiResultDocuments{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("zero documents accepted")
	}
	if err := VerifyAdoptedMultiOutputArrays(context.Background(), nil, nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, MultiResultDocuments{}, ""); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("invalid verifier accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyAdoptedMultiOutputArrays(ctx, nil, nil, auth.Identity{}, uuid.Nil, uuid.Nil, uuid.Nil, MultiResultDocuments{}, ""); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
}
