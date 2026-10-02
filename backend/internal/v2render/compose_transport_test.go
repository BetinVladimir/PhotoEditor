package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestCompositionTransportOwnsOneDigestVerifiedSnapshot(t *testing.T) {
	layer, base, proposal := compositionInputs()
	compiled, err := CompileComposition(layer, base, nil, proposal, nil, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(compiled.JSON)
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	roots, err := CompositionInputRoots(compiled, params)
	if err != nil {
		t.Fatal(err)
	}
	task := uuid.New()
	frozen, err := FreezeCompositionInputs(task, compiled, params, roots)
	if err != nil {
		t.Fatal(err)
	}
	originalOpen := frozen.Open
	opens := 0
	frozen.Open = func(ctx context.Context) (v2input.Cursor, error) {
		opens++
		if opens > 1 {
			return nil, v2input.ErrInvalid
		}
		return originalOpen(ctx)
	}
	publisher := &compositionTransportPublisher{}
	graph, err := PublishCompositionInputs(context.Background(), task, frozen, publisher)
	if err != nil || opens != 1 || graph.Built.Count != len(roots) || publisher.calls != 2 {
		t.Fatal("transport snapshot", opens, graph.Built.Count, err)
	}
	frozen.Direct[0].ArtifactID = uuid.New()
	for retry := 0; retry < 2; retry++ {
		cursor, err := graph.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for {
			_, ok, err := cursor.Next(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			count++
		}
		if count != len(roots) || graph.Direct[0] != params || opens != 1 {
			t.Fatal("graph retry borrowed mutable caller state")
		}
	}
	frozen.Open = originalOpen
	frozen.Direct = []v2input.Ref{params}
	for _, change := range []string{"task", "digest", "direct", "nil_cursor"} {
		bad := frozen
		bad.Direct = append([]v2input.Ref(nil), frozen.Direct...)
		badTask := task
		switch change {
		case "task":
			badTask = uuid.New()
		case "digest":
			bad.InputDigest = base.Source.SHA256
		case "direct":
			bad.Direct[0] = proposal
		case "nil_cursor":
			bad.Open = func(context.Context) (v2input.Cursor, error) { return nil, nil }
		}
		publisher.calls = 0
		if _, err := PublishCompositionInputs(context.Background(), badTask, bad, publisher); err == nil || publisher.calls != 0 {
			t.Fatal("invalid frozen state reached publication", change, err)
		}
	}
}
