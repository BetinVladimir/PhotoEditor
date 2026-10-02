package v2input

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

func TestSessionAnalysisRuntimeScopesTransportToDraft(t *testing.T) {
	runtime := SessionAnalysisRuntime{DB: &pgxpool.Pool{},
		Objects: &objectstore.Store{}, Bucket: "photo-test", Backend: "cuda",
		RAMLimitBytes: 24 << 30, VRAMLimitBytes: 10 << 30}
	coordinator, err := runtime.Coordinator()
	if err != nil {
		t.Fatal(err)
	}
	pending, ok := coordinator.Pending.(SessionAnalysisOrchestrator)
	if !ok || pending.TransportForDraft == nil {
		t.Fatal("admission transport factory missing")
	}
	draft := SessionAnalysisDraft{Tenant: uuid.New(), Run: uuid.New()}
	transport, ok := pending.TransportForDraft(draft).(ScopedPublisher)
	if !ok || transport.Tenant != draft.Tenant || transport.Run != draft.Run ||
		transport.Documents == nil {
		t.Fatal("transport must be scoped to its draft")
	}
	if _, err := (SessionAnalysisRuntime{DB: runtime.DB, Objects: runtime.Objects,
		Bucket: runtime.Bucket, Backend: "cuda", RAMLimitBytes: 24 << 30,
		VRAMLimitBytes: 5 << 30}).Coordinator(); err == nil {
		t.Fatal("undersized VRAM admission accepted")
	}
}
