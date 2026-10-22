package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestVerifiedKindRefusesInvalidDescriptorBeforeObjectIO(t *testing.T) {
	for _, size := range []int64{0, 1, 1048577} {
		calls := 0
		source := PlanSource{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Bytes: size, Version: "version", MediaType: "application/json"}
		_, err := readVerifiedSemanticKind(context.Background(), func() (PlanSource, error) { return source, nil }, func(context.Context, PlanSource) ([]byte, error) { calls++; return nil, nil })
		if !errors.Is(err, ErrInvalid) || calls != 0 {
			t.Fatal("invalid descriptor reached object IO", size, err, calls)
		}
	}
}
func TestVerifiedKindCancellationPrecedesAuthorityRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readVerifiedSemanticKind(ctx, func() (PlanSource, error) { t.Fatal("unexpected authority read"); return PlanSource{}, nil }, func(context.Context, PlanSource) ([]byte, error) { t.Fatal("unexpected object read"); return nil, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestVerifiedKindRejectsMissingInstallation(t *testing.T) {
	if _, err := readVerifiedSemanticKind(context.Background(), nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
