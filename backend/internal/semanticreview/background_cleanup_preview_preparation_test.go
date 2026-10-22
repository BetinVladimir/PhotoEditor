package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestBackgroundCleanupPreviewPreparationRejectsInvalidScopeBeforeAnyIO(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	for _, failure := range []string{"cancel", "identity", "target", "review", "bucket"} {
		ctx, cancel := context.WithCancel(context.Background())
		identity := who
		bucket := "fixture"
		input := BackgroundCleanupPreviewPreparation{}
		if failure == "cancel" {
			cancel()
		}
		if failure == "identity" {
			identity = auth.Identity{}
		}
		if failure == "target" {
			input.Source.TargetID = uuid.New()
		}
		if failure == "bucket" {
			bucket = "x"
		}
		objects := &forbiddenExposureObjects{}
		result, err := PrepareBackgroundCleanupPreviewRun(ctx, &pgxpool.Pool{}, objects, bucket, identity, input)
		cancel()
		if err == nil || result.RunID != uuid.Nil || objects.calls != 0 {
			t.Fatal("invalid preparation escaped", failure, err)
		}
		if failure == "cancel" && err != context.Canceled {
			t.Fatal("lost cancellation")
		}
	}
}
