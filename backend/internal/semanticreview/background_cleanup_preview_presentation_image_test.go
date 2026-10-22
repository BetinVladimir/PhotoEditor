package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestBackgroundCleanupPresentationImageCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	image, linkage, err := ReadBackgroundCleanupPresentationImage(ctx, nil, nil, "", auth.Identity{}, uuid.New(), BackgroundCleanupPreviewParams{}, []byte(`{}`), "", "")
	if err != context.Canceled || image.Image.Ref.ArtifactID != uuid.Nil || linkage.Document.TaskID != uuid.Nil {
		t.Fatal("cancelled image escaped", err)
	}
}
