package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPrepareDraftResourcesAndLimits(t *testing.T) {
	snapshot, raw, policy, configs := freezeFixture(t)
	_, frozenRaw, sha, err := CompileFrozenInput(snapshot, raw, policy, configs)
	if err != nil {
		t.Fatal(err)
	}
	frozen := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	project, session := uuid.New(), uuid.New()
	_, _, params, err := CompilePrepareProof(project, session, frozen, frozenRaw, raw, configs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompilePreparePlan(uuid.New(), project, session, v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(params)}, frozen, frozenRaw, raw, configs)
	if err != nil {
		t.Fatal(err)
	}
	limits := PrepareResourceLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 8 << 20}
	frozenTask, err := plan.FreezeTask(plan.Roots(), limits)
	if err != nil {
		t.Fatal(err)
	}
	ownedDraft, err := frozenTask.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	ownedDraft.ExecutionProfile[0] = 'x'
	stableDraft, err := frozenTask.DraftTask()
	if err != nil || stableDraft.ExecutionProfile[0] != '{' {
		t.Fatal("borrowed frozen profile")
	}
	if _, err := frozenTask.PublishInputs(context.Background(), nil); err == nil {
		t.Fatal("nil publisher accepted")
	}
	if _, err := (FrozenPrepareTask{}).DraftTask(); err == nil {
		t.Fatal("zero frozen task accepted")
	}
	task, err := plan.DraftTask(plan.Roots(), limits)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := plan.InputClosure(plan.Roots())
	if err != nil || task.InputDigest != closure.InputDigest || task.ID != plan.TaskID() || task.Operation != "v2.delivery.prepare" {
		t.Fatal("task drift")
	}
	var profile struct {
		Backend  string                  `json:"backend"`
		Renderer string                  `json:"renderer_version"`
		Estimate prepareResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(task.ExecutionProfile, &profile) != nil || profile.Backend != "cpu" || profile.Renderer != "v2.2" || profile.Estimate.VRAMBytes != 0 || profile.Estimate.RAMBytes < 512<<20 {
		t.Fatal("invalid reservation")
	}
	for _, resource := range []string{"ram", "scratch", "output"} {
		low := limits
		switch resource {
		case "ram":
			low.RAMBytes = profile.Estimate.RAMBytes - 1
		case "scratch":
			low.ScratchBytes = profile.Estimate.ScratchBytes - 1
		case "output":
			low.OutputBytes = profile.Estimate.OutputBytes - 1
		}
		if _, err := plan.DraftTask(plan.Roots(), low); err == nil {
			t.Fatal("underreserved", resource)
		}
	}
	task.ExecutionProfile[0] = 'x'
	if _, err := plan.DraftTask(plan.Roots(), limits); err != nil {
		t.Fatal("borrowed profile")
	}
}
