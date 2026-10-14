package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestImportMaskCandidateArchiveReaderRejectsUnboundAndUnsupportedReads(t *testing.T) {
	source := SQLImportMaskCandidateArchiveReader{}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("candidate"))}
	for _, contract := range []string{"Artifact", "MaskRef", "", "EditingRecipeV3", "Override"} {
		if _, err := source.lookup(context.Background(), ref, contract); err == nil {
			t.Fatalf("accepted unbound %s read", contract)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.lookup(ctx, ref, "Override"); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
	if _, err := source.Read(context.Background(), ref, "Override"); err == nil {
		t.Fatal("accepted archive read without object store")
	}
}
