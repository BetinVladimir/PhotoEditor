package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"reflect"
	"testing"
)

func TestInitialDefectAdvanceCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := (nativePreparationBootstrap{}).advanceInitialPersonDefect(ctx, auth.Identity{}, nativeInputRestoredFaceProducers{}, nativePersonInputAuthority{}, TransferPreparation{}, 1, nil)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(out, nativeInitialDefectAdvance{}) {
		t.Fatal("cancelled advancement exposed protection", err)
	}
}
