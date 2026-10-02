package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"testing"
)

type preparedDocuments struct{ nodes []PublishedIngestNode }

func (d preparedDocuments) Publish(context.Context, uuid.UUID, IngestSpec) ([]PublishedIngestNode, error) {
	return d.nodes, nil
}

type inspectionWriter struct {
	calls  []PreparedTask
	failAt int
	seals  int
}

func (w *inspectionWriter) Materialize(_ context.Context, _, _ uuid.UUID, task PreparedTask, metadata *uuid.UUID) (bool, error) {
	if metadata != nil {
		panic("initial stage passed metadata")
	}
	w.calls = append(w.calls, task)
	if len(w.calls) == w.failAt {
		return false, errors.New("database unavailable")
	}
	return true, nil
}
func (w *inspectionWriter) SealInspection(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	w.seals++
	return true, nil
}
func TestInspectionCoordinatorDoesNotSealPartialBatch(t *testing.T) {
	spec, registry := ingestFixture(t)
	plan, _ := CompileIngest(spec, registry)
	documents := preparedDocuments{}
	for _, node := range plan.Nodes {
		hash := sha256.Sum256(node.Params)
		documents.nodes = append(documents.nodes, PublishedIngestNode{Node: node, Params: review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(hash[:])}})
	}
	writer := &inspectionWriter{failAt: 2}
	c := IngestInspectionCoordinator{Documents: documents, Tasks: writer}
	tenant := uuid.New()
	if err := c.PrepareInspection(context.Background(), tenant, spec); err == nil || writer.seals != 0 {
		t.Fatal("partial batch sealed")
	}
	firstID := writer.calls[0].ID
	writer.failAt = 0
	writer.calls = nil
	if err := c.PrepareInspection(context.Background(), tenant, spec); err != nil || len(writer.calls) != 4 || writer.seals != 1 || writer.calls[0].ID != firstID {
		t.Fatal("resume failed", err)
	}
	for _, task := range writer.calls {
		if task.Operation == "asset.decode" {
			t.Fatal("decode created without metadata")
		}
	}
	writer.calls = nil
	writer.seals = 0
	documents.nodes[1].Params.SHA256 = "bad"
	c.Documents = documents
	if err := c.PrepareInspection(context.Background(), tenant, spec); err == nil || len(writer.calls) != 0 || writer.seals != 0 {
		t.Fatal("invalid params reached SQL")
	}
}
