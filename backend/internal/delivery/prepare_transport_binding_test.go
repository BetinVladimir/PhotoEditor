package delivery

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type bindingPublisher struct{}

func (bindingPublisher) Publish(_ context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	return v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte(fmt.Sprintf("%s:%d:%s", contract, ordinal, digest(body)))), SHA256: digest(body)}, nil
}

func TestPrepareTransportBindingUsesOwnedCursorAndRejectsSubstitution(t *testing.T) {
	i, session, catalog, items := createDocumentsFixture(t)
	docs, err := i.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := docs.Plan.FreezeTask(docs.Plan.Roots(), PrepareResourceLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := frozen.PublishInputs(context.Background(), bindingPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	graph.Open = func(context.Context) (v2input.Cursor, error) {
		t.Fatal("untrusted graph cursor invoked")
		return nil, v2input.ErrInvalid
	}
	bound, err := frozen.BindPublishedInputs(context.Background(), graph)
	if err != nil || bound.Open == nil {
		t.Fatalf("bind: %v", err)
	}
	graph.Built.Chunks[0].SHA256 = digest([]byte("mutated receipt"))
	if bound.Built.Chunks[0].SHA256 == graph.Built.Chunks[0].SHA256 {
		t.Fatal("borrowed receipt slice")
	}
	for _, kind := range []string{"task", "count", "digest", "chunk", "manifest", "direct", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			candidate, err := frozen.PublishInputs(context.Background(), bindingPublisher{})
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "task":
				candidate.ID = uuid.New()
			case "count":
				candidate.Built.Count++
			case "digest":
				candidate.Built.SHA256 = digest([]byte("other inventory"))
			case "chunk":
				candidate.Built.Chunks[0].SHA256 = digest([]byte("other chunk"))
			case "manifest":
				candidate.Built.Manifest.SHA256 = digest([]byte("other manifest"))
			case "direct":
				candidate.Direct[0].ArtifactID = uuid.New()
			case "duplicate":
				candidate.Built.Manifest.ArtifactID = candidate.Built.Chunks[0].ArtifactID
			}
			if _, err := frozen.BindPublishedInputs(context.Background(), candidate); err == nil {
				t.Fatal("substituted transport accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := frozen.BindPublishedInputs(ctx, bound); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
}
