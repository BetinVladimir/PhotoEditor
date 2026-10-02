package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestFrozenCompositionInventoryCopiesAndReopensForRetries(t *testing.T) {
	layer, base, proposal := compositionInputs()
	compiled, _ := CompileComposition(layer, base, nil, proposal, nil, 2, 1)
	sha := sha256.Sum256(compiled.JSON)
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sha[:])}
	entries, _ := CompositionInputRoots(compiled, params)
	task := uuid.New()
	frozen, err := FreezeCompositionInputs(task, compiled, params, entries)
	if err != nil || len(frozen.Direct) != 1 || frozen.Direct[0] != params || len(frozen.InputDigest) != 64 {
		t.Fatal(frozen, err)
	}
	expected := append([]v2input.Entry(nil), entries...)
	entries[0].Contract = "mutated"
	for i := 0; i < 2; i++ {
		cursor, err := frozen.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range expected {
			got, ok, err := cursor.Next(context.Background())
			if err != nil || !ok || got != want {
				t.Fatal(got, want, err)
			}
		}
		if _, ok, err := cursor.Next(context.Background()); ok || err != nil {
			t.Fatal("cursor EOF", err)
		}
	}
	transportCursor, _ := frozen.Open(context.Background())
	publisher := &compositionTransportPublisher{}
	built, err := v2input.BuildInputClosure(context.Background(), task, transportCursor, publisher)
	expectedSHA, _ := canonical.Hash(expected)
	if err != nil || built.Count != len(expected) || built.SHA256 != expectedSHA || len(built.Chunks) != 1 || publisher.calls != 2 {
		t.Fatal("frozen transport mismatch", built, err)
	}
	changed, err := FreezeCompositionInputs(uuid.New(), compiled, params, expected)
	if err != nil || changed.InputDigest == frozen.InputDigest {
		t.Fatal("task identity missing from digest")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := frozen.Open(ctx); err == nil {
		t.Fatal("cancelled open")
	}
	cursor, _ := frozen.Open(context.Background())
	if _, _, err := cursor.Next(ctx); err == nil {
		t.Fatal("cancelled cursor")
	}
}

func TestFrozenCompositionInventoryRejectsMissingConflictingAndDuplicateRoots(t *testing.T) {
	layer, base, proposal := compositionInputs()
	compiled, _ := CompileComposition(layer, base, nil, proposal, nil, 2, 1)
	sha := sha256.Sum256(compiled.JSON)
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sha[:])}
	entries, _ := CompositionInputRoots(compiled, params)
	for _, bad := range [][]v2input.Entry{entries[:len(entries)-1], append(append([]v2input.Entry(nil), entries...), entries[0])} {
		if _, err := FreezeCompositionInputs(uuid.New(), compiled, params, bad); err == nil {
			t.Fatal("invalid inventory")
		}
	}
	entries[0].Contract = "Artifact"
	if _, err := FreezeCompositionInputs(uuid.New(), compiled, params, entries); err == nil {
		t.Fatal("root type erased")
	}
}

type compositionTransportPublisher struct {
	calls   int
	firstID uuid.UUID
}

func (p *compositionTransportPublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	p.calls++
	sum := sha256.Sum256(body)
	id := uuid.New()
	if p.calls == 1 && p.firstID != uuid.Nil {
		id = p.firstID
	}
	return v2input.Ref{ArtifactID: id, SHA256: hex.EncodeToString(sum[:])}, nil
}
