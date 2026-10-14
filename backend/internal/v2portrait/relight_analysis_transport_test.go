package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"testing"
)

type analysisTransportPublisher struct{ calls int }

func (p *analysisTransportPublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	p.calls++
	sum := sha256.Sum256(body)
	return v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte(contract+strconv.Itoa(ordinal))), SHA256: hex.EncodeToString(sum[:])}, nil
}
func TestAnalysisTransportBindsCompleteInventoryAndReplayCursor(t *testing.T) {
	request := analysisPlanFixture(t)
	plan, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &analysisTransportPublisher{}
	graph, err := plan.PublishInputClosure(context.Background(), publisher)
	if err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 2 || graph.ID != plan.snapshot.Task || graph.Built.Count != len(plan.snapshot.Inputs) {
		t.Fatal("incomplete transport")
	}
	for retry := 0; retry < 2; retry++ {
		cursor, err := graph.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range plan.snapshot.Inputs {
			entry, ok, err := cursor.Next(context.Background())
			if err != nil || !ok || entry != expected {
				t.Fatal("cursor changed", err)
			}
		}
		_, ok, err := cursor.Next(context.Background())
		if ok || err != nil {
			t.Fatal("unexpected input", err)
		}
	}
	for _, fault := range []string{"count", "entries_hash", "chunk_hash", "manifest_hash", "extra_chunk"} {
		built := graph.Built
		built.Chunks = append([]v2input.Ref(nil), built.Chunks...)
		switch fault {
		case "count":
			built.Count++
		case "entries_hash":
			built.SHA256 = "invalid"
		case "chunk_hash":
			built.Chunks[0].SHA256 = "invalid"
		case "manifest_hash":
			built.Manifest.SHA256 = "invalid"
		case "extra_chunk":
			built.Chunks = append(built.Chunks, built.Chunks[0])
		}
		if _, err := plan.BindInputClosure(context.Background(), built); err == nil {
			t.Fatal("accepted", fault)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := plan.PublishInputClosure(ctx, publisher); err != context.Canceled {
		t.Fatal(err)
	}
	if publisher.calls != 2 {
		t.Fatal("cancelled publication wrote objects")
	}
}
