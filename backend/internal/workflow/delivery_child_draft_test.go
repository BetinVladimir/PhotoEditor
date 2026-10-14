package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

type deliveryDraftRow struct{ accepted bool }

func (r deliveryDraftRow) Scan(dest ...any) error { *dest[0].(*bool) = r.accepted; return nil }

type deliveryDraftTx struct {
	accepted bool
	args     []any
	calls    int
}

func (tx *deliveryDraftTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	tx.calls++
	tx.args = args
	return deliveryDraftRow{tx.accepted}
}

func TestDeliveryChildDraftBindsFrozenShellAndLease(t *testing.T) {
	plan, parent, child, raw, base := deliveryExportPlanFixture(t, nil)
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	node, err := CompileDeliveryInitialNode(plan, parent, child, raw, base.TargetICCSHA256, registry)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	resolved := []v2input.Entry{
		{Ref: child.Binding.Source, Contract: "WorkingImage"}, {Ref: child.Recipe, Contract: "EditingRecipeV3"}, {Ref: child.Preset.Config, Contract: "ExportPresetConfig"},
		{Ref: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, Contract: "NpyArray"},
	}
	if image.Alpha != nil {
		resolved = append(resolved, v2input.Entry{Ref: v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, Contract: "NpyArray"})
	}
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(node.Params)}
	frozen, err := FreezeDeliveryInitialTask(plan, parent, child, raw, base.TargetICCSHA256, registry, params, resolved, DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	workflow, _ := v2input.WorkflowID(parent)
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute), Tenant: uuid.New(), Run: parent, Workflow: workflow, Package: uuid.New(), ParentTask: plan.TaskID(), Cursor: 0, SlotCount: 1}
	exportID := uuid.New()
	tx := &deliveryDraftTx{accepted: true}
	reserved := int64(64 << 20)
	if err := frozen.CreateDraft(context.Background(), tx, lease, plan, child, exportID, reserved); err != nil {
		t.Fatal(err)
	}
	if len(tx.args) != 9 || tx.args[0] != lease.Expansion || tx.args[1] != lease.Token || tx.args[2] != 0 || tx.args[4] != exportID || tx.args[8] != reserved {
		t.Fatal("wrong lease/shell binding")
	}
	var task v2input.DraftTask
	if json.Unmarshal([]byte(tx.args[6].(string)), &task) != nil || task.ID != node.ID || task.ParamsSHA != params.SHA256 || tx.args[7] != string(node.Params) {
		t.Fatal("params/task drift")
	}
	tx.accepted = false
	if err := frozen.CreateDraft(context.Background(), tx, lease, plan, child, exportID, reserved); err == nil {
		t.Fatal("SQL refusal accepted")
	}
	calls := tx.calls
	if err := frozen.CreateDraft(context.Background(), tx, lease, plan, child, exportID, 1); err == nil || tx.calls != calls {
		t.Fatal("insufficient reservation reached SQL")
	}
	foreign := child
	foreign.RunID = uuid.New()
	if err := frozen.CreateDraft(context.Background(), tx, lease, plan, foreign, exportID, reserved); err == nil || tx.calls != calls {
		t.Fatal("foreign child reached SQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := frozen.CreateDraft(ctx, tx, lease, plan, child, exportID, reserved); err != context.Canceled || tx.calls != calls {
		t.Fatal("cancelled admission reached SQL")
	}
}
