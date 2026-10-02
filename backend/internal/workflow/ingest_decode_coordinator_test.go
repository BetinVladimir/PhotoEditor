package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"testing"
)

type decodeWriter struct {
	begins, writes, seals int
	failAt                int
	metadata              []uuid.UUID
}

func (w *decodeWriter) BeginDecode(context.Context, uuid.UUID, uuid.UUID, IngestDecodePlan) (bool, error) {
	w.begins++
	return true, nil
}
func (w *decodeWriter) Materialize(_ context.Context, _, _ uuid.UUID, _ PreparedTask, metadata *uuid.UUID) (bool, error) {
	w.writes++
	if metadata == nil {
		panic("missing metadata")
	}
	w.metadata = append(w.metadata, *metadata)
	if w.writes == w.failAt {
		return false, errors.New("DB failure")
	}
	return true, nil
}
func (w *decodeWriter) SealDecode(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	w.seals++
	return true, nil
}
func TestDecodeCoordinatorBarrierOnPartialFailure(t *testing.T) {
	writer := &decodeWriter{failAt: 2}
	coordinator := IngestDecodeCoordinator{Tasks: writer}
	tenant, run := uuid.New(), uuid.New()
	plan := IngestDecodePlan{Ready: true, Tasks: []IngestDecodeTask{{Metadata: review.ArtifactRef{ID: uuid.New()}}, {Metadata: review.ArtifactRef{ID: uuid.New()}}}}
	if ok, err := coordinator.Build(context.Background(), tenant, run, plan); ok || err == nil || writer.seals != 0 {
		t.Fatal("partial graph sealed")
	}
	writer.failAt = 0
	writer.writes = 0
	writer.metadata = nil
	if ok, err := coordinator.Build(context.Background(), tenant, run, plan); !ok || err != nil || writer.seals != 1 || writer.metadata[0] != plan.Tasks[0].Metadata.ID {
		t.Fatal("resume lost metadata", err)
	}
	writer.begins = 0
	plan.Ready = false
	if ok, err := coordinator.Build(context.Background(), tenant, run, plan); ok || err != nil || writer.begins != 0 {
		t.Fatal("waiting plan mutated DB")
	}
	plan.Ready = true
	plan.Tasks = nil
	plan.Failures = []IngestItemFailure{{Ordinal: 1}}
	writer.writes = 0
	if ok, err := coordinator.Build(context.Background(), tenant, run, plan); !ok || err != nil || writer.writes != 0 {
		t.Fatal("all-failed plan created decode", err)
	}
}
