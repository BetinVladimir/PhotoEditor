package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestRemovalPreviewProviderRequiresDependencies(t *testing.T) {
	if _, err := NewRemovalPreviewProvider(nil, nil, "", nil, nil, "a", "b"); err == nil {
		t.Fatal("incomplete provider installed")
	}
	var provider *RemovalPreviewProvider
	got, err := provider.ReadPreview(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), PreviewRequest{})
	if err != ErrUnavailable || got.SchemaVersion != 0 {
		t.Fatal("nil provider returned preview", err)
	}
}

func TestRemovalPreviewProviderCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var provider *RemovalPreviewProvider
	if _, err := provider.ReadPreview(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), PreviewRequest{}); err != context.Canceled {
		t.Fatal("cancel ignored", err)
	}
}
