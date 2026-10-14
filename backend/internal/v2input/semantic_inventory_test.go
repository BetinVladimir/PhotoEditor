package v2input

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Each native stage must bind catalog descriptors to its own complete frozen
// inventory before trusting retained object metadata for SQL admission.
func assertSemanticInventoryBoundary(t *testing.T, verify func(context.Context, []Entry) error, inputs []Entry, params Ref) {
	t.Helper()
	entries := append(append([]Entry{}, inputs...), Entry{Ref: params, Contract: "Params"})
	if err := verify(context.Background(), entries); err != nil {
		t.Fatal("exact inventory rejected", err)
	}
	reversed := append([]Entry{}, entries...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if err := verify(context.Background(), reversed); err != nil {
		t.Fatal("catalog order changed task identity", err)
	}
	for _, kind := range []string{"missing", "extra", "duplicate", "contract", "sha"} {
		bad := append([]Entry{}, entries...)
		switch kind {
		case "missing":
			bad = bad[:len(bad)-1]
		case "extra":
			extra := bad[0]
			extra.Ref.ArtifactID = uuid.New()
			bad = append(bad, extra)
		case "duplicate":
			bad[0] = bad[1]
		case "contract":
			bad[0].Contract = "Params"
		case "sha":
			bad[0].Ref.SHA256 = params.SHA256
		}
		if err := verify(context.Background(), bad); err == nil {
			t.Fatal("changed catalog inventory accepted", kind)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verify(ctx, entries); err != context.Canceled {
		t.Fatal("inventory cancellation lost", err)
	}
}
