package interchange

import (
	"context"
	"errors"
	"testing"
)

func TestImportMaskCandidateParsedSourceRejectsAbsentAuthorityBeforeIO(t *testing.T) {
	source := SQLImportMaskCandidateParsedSource{}
	if _, _, err := source.ResolveReviewParsed(context.Background()); err == nil {
		t.Fatal("accepted an unconfigured parsed-object authority")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := source.ResolveReviewParsed(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
}
