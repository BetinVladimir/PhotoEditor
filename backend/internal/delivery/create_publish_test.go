package delivery

import (
	"context"
	"errors"
	"fmt"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type createPublisherStub struct {
	calls  []CreateDocument
	invoke func(CreateDocument) (v2input.Ref, error)
}

func (s *createPublisherStub) PublishCreateDocument(_ context.Context, _ CreateIdentity, doc CreateDocument) (v2input.Ref, error) {
	s.calls = append(s.calls, doc)
	if s.invoke != nil {
		return s.invoke(doc)
	}
	return doc.Ref, nil
}
func TestCreatePublicationOrderAndOwnedBytes(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	docs, err := intent.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	stub := &createPublisherStub{}
	stub.invoke = func(doc CreateDocument) (v2input.Ref, error) { docs.Params.Body[0] = 'x'; return doc.Ref, nil }
	if err := intent.PublishDocuments(context.Background(), docs, stub); err != nil {
		t.Fatal(err)
	}
	if len(stub.calls) != 5 || stub.calls[0].Purpose != "v30_delivery_snapshot" || stub.calls[1].Purpose != "v30_delivery_preset" || stub.calls[4].Purpose != "task_params" || stub.calls[4].Body[0] != '{' {
		t.Fatal("publication ordering/ownership")
	}
}
func TestCreatePublicationAllPreflightBeforeIOAndStopOnFailure(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	docs, err := intent.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := docs
	corrupt.Params.Body = []byte("broken")
	stub := &createPublisherStub{}
	if err := intent.PublishDocuments(context.Background(), corrupt, stub); err == nil || len(stub.calls) != 0 {
		t.Fatal("corruption reached object IO")
	}
	expected := errors.New("object failure")
	stub.invoke = func(doc CreateDocument) (v2input.Ref, error) { return v2input.Ref{}, expected }
	if err := intent.PublishDocuments(context.Background(), docs, stub); !errors.Is(err, expected) || len(stub.calls) != 1 {
		t.Fatal("did not stop on failure", err)
	}
	stub = &createPublisherStub{invoke: func(doc CreateDocument) (v2input.Ref, error) { return v2input.Ref{}, nil }}
	if err := intent.PublishDocuments(context.Background(), docs, stub); err == nil || len(stub.calls) != 1 {
		t.Fatal("foreign receipt accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stub = &createPublisherStub{}
	if err := intent.PublishDocuments(ctx, docs, stub); err == nil || len(stub.calls) != 0 {
		t.Fatal("cancelled IO")
	}
}

func TestCreatePublicationCancellationAtEveryDocument(t *testing.T) {
    for stop := 1; stop <= 5; stop++ {
        for _, outcome := range []string{"matching", "foreign", "failed"} {
            t.Run(fmt.Sprintf("document_%d_%s", stop, outcome), func(t *testing.T) {
                intent, session, catalog, items := createDocumentsFixture(t)
                docs, err := intent.CompileDocuments(context.Background(), session, catalog, items)
                if err != nil { t.Fatal(err) }
                ctx, cancel := context.WithCancel(context.Background())
                defer cancel()
                primary := errors.New("publisher failure")
                stub := &createPublisherStub{}
                stub.invoke = func(doc CreateDocument) (v2input.Ref, error) {
                    if len(stub.calls) != stop { return doc.Ref, nil }
                    cancel()
                    switch outcome {
                    case "foreign": return v2input.Ref{}, nil
                    case "failed": return v2input.Ref{}, primary
                    default: return doc.Ref, nil
                    }
                }
                err = intent.PublishDocuments(ctx, docs, stub)
                expected := error(context.Canceled)
                if outcome == "failed" { expected = primary }
                if err != expected || len(stub.calls) != stop {
                    t.Fatal("continued publication or lost primary error", err, len(stub.calls))
                }
            })
        }
    }
}
