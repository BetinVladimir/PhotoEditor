package semanticproposal

import (
	"context"
	"testing"
)

func TestNativeRecoveryDispatcherCancellationBeforeProjectDiscovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, dispatcher := range []*SQLNativeParentRecoveryDispatcher{nil, {}} {
		if done, err := dispatcher.ProcessOne(ctx); done || err != context.Canceled {
			t.Fatal("cancelled dispatcher discovered work", err)
		}
		if done, err := dispatcher.ProcessOne(context.Background()); done || err == nil {
			t.Fatal("unconfigured dispatcher reached SQL")
		}
	}
}
