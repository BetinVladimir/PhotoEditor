package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRetouchPhysicalPreviewCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := ReadPhysicalRetouchPreview(ctx, nil, nil, "", auth.Identity{}, uuid.New(), RetouchPreviewParams{}, v2input.Ref{}, nil, "", "")
	if err != context.Canceled || got.verified {
		t.Fatal("cancelled physical preview escaped", err)
	}
}

func TestRetouchPhysicalPreviewMissingSourceBeforeIO(t *testing.T) {
	got, err := ReadPhysicalRetouchPreview(context.Background(), nil, nil, "", auth.Identity{}, uuid.New(), RetouchPreviewParams{}, v2input.Ref{}, nil, "", "")
	if err != ErrInvalid || got.verified {
		t.Fatal("unbound physical preview escaped", err)
	}
}
