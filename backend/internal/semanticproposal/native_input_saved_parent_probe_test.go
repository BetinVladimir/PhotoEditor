package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestSavedParentProbeRefusesIncompleteScope(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, current := range []context.Context{ctx, context.Background()} {
		exists, err := (SQLTransferSourceReader{}).hasNativeInputSavedParent(current, auth.Identity{}, Request{})
		if err == nil || exists {
			t.Fatal("invalid scope exposed saved parent", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}
