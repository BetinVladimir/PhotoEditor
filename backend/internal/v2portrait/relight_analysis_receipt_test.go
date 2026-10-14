package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestAnalysisReceiptBindsOriginalPlanAndTransport(t *testing.T) {
	request := analysisPlanFixture(t)
	plan, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := plan.PublishInputClosure(context.Background(), &analysisTransportPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := canonical.Marshal(map[string]any{"manifest": graph.Built.Manifest, "chunks": graph.Built.Chunks, "count": graph.Built.Count, "sha256": graph.Built.SHA256})
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := plan.Descriptor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := canonical.Hash(json.RawMessage(descriptor))
	if err != nil {
		t.Fatal(err)
	}
	original := RelightAnalysisReceipt{SchemaVersion: 1, Run: plan.snapshot.Run, Workflow: plan.snapshot.Workflow, Task: plan.snapshot.Task, State: "queued", Actor: request.Actor, Session: request.Session, Operation: request.Operation, DescriptorSHA: digest, Qualification: request.Runtime.Qualification, Transport: transport}
	for _, fault := range []string{"none", "run", "workflow", "task", "actor", "session", "operation", "digest", "qualification", "transport", "state", "extra"} {
		changed := original
		switch fault {
		case "run":
			changed.Run = uuid.New()
		case "workflow":
			changed.Workflow = uuid.New()
		case "task":
			changed.Task = uuid.New()
		case "actor":
			changed.Actor = uuid.New()
		case "session":
			changed.Session = uuid.New()
		case "operation":
			changed.Operation = "mask.person"
		case "digest":
			changed.DescriptorSHA = "invalid"
		case "qualification":
			changed.Qualification.ArtifactID = uuid.New()
		case "transport":
			changed.Transport = json.RawMessage("{}")
		case "state":
			changed.State = "running"
		}
		body, err := canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if fault == "extra" {
			var fields map[string]any
			json.Unmarshal(body, &fields)
			fields["extra"] = true
			body, err = canonical.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err = parseAnalysisReceipt(context.Background(), plan, request, body)
		if (err == nil) != (fault == "none") {
			t.Fatal(fault, err)
		}
	}
}
func TestAnalysisReceiptReadRefusesForeignPrincipalBeforeDB(t *testing.T) {
	request := analysisPlanFixture(t)
	db := &noAnalysisDraftDB{}
	c := RelightAnalysisCoordinator{DB: db}
	who := auth.Identity{UserID: uuid.New(), SessionID: request.Session}
	if _, err := c.ReadQueuedReceipt(context.Background(), who, request); err == nil || db.calls != 0 {
		t.Fatal("foreign principal reached DB")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.ReadQueuedReceipt(ctx, auth.Identity{UserID: request.Actor, SessionID: request.Session}, request); err != context.Canceled || db.calls != 0 {
		t.Fatal("cancelled recovery reached DB", err)
	}
}
