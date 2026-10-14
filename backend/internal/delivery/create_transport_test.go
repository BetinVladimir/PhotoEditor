package delivery

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestCreateTransportPreflightAndFullInventoryResources(t *testing.T) {
	i, session, catalog, items := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	limits := PrepareResourceLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 8 << 20}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Service{}).StageCreateTransport(ctx, who, i.tenant, i.Request(), i.key, "media", nil, nil, limits); err != context.Canceled {
		t.Fatalf("cancel preflight: %v", err)
	}
	if _, err := (Service{}).StageCreateTransport(context.Background(), who, i.tenant, i.Request(), i.key, "media", nil, nil, limits); err == nil {
		t.Fatal("nil publisher accepted")
	}
	docs, err := i.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	roots := docs.Plan.Roots()
	base, err := docs.Plan.FreezeTask(roots, limits)
	if err != nil {
		t.Fatal(err)
	}
	leaf := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("leaf"))}, Contract: "WorkingPixels"}
	larger, err := docs.Plan.FreezeTask(append(roots, leaf, leaf), limits)
	if err != nil {
		t.Fatal(err)
	}
	first, err := base.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	second, err := larger.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	var a, b struct {
		Estimate prepareResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(first.ExecutionProfile, &a) != nil || json.Unmarshal(second.ExecutionProfile, &b) != nil {
		t.Fatal("invalid profile")
	}
	if b.Estimate.RAMBytes-a.Estimate.RAMBytes != 1024 || b.Estimate.ScratchBytes-a.Estimate.ScratchBytes != 1024 {
		t.Fatal("unique transitive inventory not accounted")
	}
	limits.RAMBytes = a.Estimate.RAMBytes
	if _, err := docs.Plan.FreezeTask(append(roots, leaf), limits); err == nil {
		t.Fatal("inventory exceeds memory limit")
	}
}
