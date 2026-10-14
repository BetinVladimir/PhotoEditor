package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestAnalysisIntentRejectsPrivatePinsAndIncompleteSource(t *testing.T) {
	request := analysisPlanFixture(t)
	intent := RelightAnalysisIntent{SchemaVersion: 1, Snapshot: request.Snapshot, Base: request.Base, Operation: request.Operation}
	body, err := canonical.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRelightAnalysisIntent(body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"model_lock_sha", "runtime_lock_sha", "qualification", "resource_estimate", "tenant_id", "actor_id", "run_id"} {
		var fields map[string]any
		json.Unmarshal(body, &fields)
		fields[field] = "injected"
		changed, err := canonical.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeRelightAnalysisIntent(changed); err == nil {
			t.Fatal("private field accepted", field)
		}
	}
	intent.Operation = "image.relight"
	body, _ = canonical.Marshal(intent)
	if _, err := DecodeRelightAnalysisIntent(body); err == nil {
		t.Fatal("unsupported operation accepted")
	}
}
func TestAnalysisServiceOwnsAcceptanceAndRejectsIntentBeforeIO(t *testing.T) {
	request, _, expected := analysisQualificationFixture(t)
	db := &noAnalysisDraftDB{}
	coordinator := RelightAnalysisCoordinator{DB: db, Reader: noAnalysisPublicationObjects{}, Writer: noAnalysisPublicationObjects{}, Bucket: "photos", Expected: expected}
	service, err := NewRelightAnalysisService(coordinator, request.Tenant, request.Project, request.Runtime, request.Capacity)
	if err != nil {
		t.Fatal(err)
	}
	expected.RequiredChecks[0] = "changed"
	for key := range expected.TestFixtureSHAs {
		delete(expected.TestFixtureSHAs, key)
	}
	if service.coordinator.Expected.RequiredChecks[0] != "quality" || len(service.coordinator.Expected.TestFixtureSHAs) == 0 {
		t.Fatal("borrowed acceptance policy")
	}
	who := auth.Identity{UserID: request.Actor, SessionID: request.Session}
	intent := RelightAnalysisIntent{SchemaVersion: 1, Snapshot: request.Snapshot, Base: request.Base, Operation: request.Operation}
	for _, fault := range []string{"key", "actor", "snapshot", "operation", "cancel"} {
		changed := intent
		identity := who
		key := request.Key
		ctx, cancel := context.WithCancel(context.Background())
		switch fault {
		case "key":
			key = ""
		case "actor":
			identity.UserID = uuid.Nil
		case "snapshot":
			changed.Snapshot = uuid.Nil
		case "operation":
			changed.Operation = "mask.person"
		case "cancel":
			cancel()
		}
		_, err := service.SubmitIntent(ctx, identity, key, changed)
		cancel()
		if err == nil || db.calls != 0 {
			t.Fatal("invalid intent reached IO", fault, err)
		}
	}
}
