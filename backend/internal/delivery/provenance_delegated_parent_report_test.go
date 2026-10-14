package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedParentReportReaderRequiresScope(t *testing.T) {
	reader := DelegatedParentReportReader{}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if _, err := reader.ReadSigningImageReport(context.Background(), ref); err == nil {
		t.Fatal("unconfigured parent report reader accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.ReadSigningImageReport(ctx, ref); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
