package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAnalysisDescriptorOwnsTaskAndCompleteInventory(t *testing.T) {
	for _, operation := range []string{"depth.predict", "mask.person"} {
		request := analysisPlanFixture(t)
		request.Operation = operation
		plan, err := CompileRelightAnalysisPlan(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		task, err := plan.DraftTask(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if task.Operation != operation || task.ResourceClass != "gpu" || task.ParamsArtifactID != plan.snapshot.ParamsDocument {
			t.Fatal("wrong task binding")
		}
		expected, err := canonical.Hash(struct {
			Task      uuid.UUID       `json:"task"`
			Operation string          `json:"operation"`
			Version   string          `json:"version"`
			Entries   []v2input.Entry `json:"entries"`
		}{task.ID, operation, "1", plan.Snapshot().Inputs})
		if err != nil || task.InputDigest != expected {
			t.Fatal("digest differs from worker ABI", err)
		}
		first, err := plan.Descriptor(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		task.ExecutionProfile[0] = 'x'
		second, err := plan.Descriptor(context.Background())
		if err != nil || !bytes.Equal(first, second) {
			t.Fatal("task leaked owned bytes", err)
		}
		var descriptor struct {
			SchemaVersion int               `json:"schema_version"`
			Task          v2input.DraftTask `json:"task"`
			Entries       []v2input.Entry   `json:"entries"`
			ReservedBytes int64             `json:"reserved_bytes"`
		}
		if json.Unmarshal(first, &descriptor) != nil || descriptor.SchemaVersion != 1 || len(descriptor.Entries) != len(plan.snapshot.Inputs) || descriptor.ReservedBytes != plan.snapshot.ReservedBytes {
			t.Fatal("incomplete descriptor")
		}
	}
}
func TestAnalysisDescriptorRefusesInvalidPlanAndCancellation(t *testing.T) {
	if _, err := (RelightAnalysisPlan{}).Descriptor(context.Background()); err == nil {
		t.Fatal("empty plan accepted")
	}
	request := analysisPlanFixture(t)
	for _, fault := range []string{"task", "workflow", "params_id", "params_sha", "inventory", "operation", "source_body"} {
		plan, err := CompileRelightAnalysisPlan(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "task":
			plan.snapshot.Task = uuid.New()
		case "workflow":
			plan.snapshot.Workflow = uuid.New()
		case "params_id":
			plan.snapshot.ParamsDocument = uuid.New()
		case "params_sha":
			plan.snapshot.ParamsSHA = "invalid"
		case "inventory":
			plan.snapshot.Inputs = plan.snapshot.Inputs[:1]
		case "source_body":
			plan.snapshot.SourceJCS = "{}"
		case "operation":
			plan.snapshot.Operation = "image.relight"
		}
		if _, err := plan.Descriptor(context.Background()); err == nil {
			t.Fatal("accepted", fault)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (RelightAnalysisPlan{}).Descriptor(ctx); err != context.Canceled {
		t.Fatal(err)
	}
}
