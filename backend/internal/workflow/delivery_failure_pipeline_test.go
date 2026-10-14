package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

type deliveryFailureWriter struct {
	calls   int
	fail    bool
	witness delivery.FailureWitness
}

func (w *deliveryFailureWriter) FailPackage(_ context.Context, _ delivery.ExpansionLease, _ delivery.PreparePlan, v delivery.FailureWitness) error {
	w.calls++
	w.witness = v
	if w.fail {
		return errors.New("failure commit")
	}
	return nil
}
func TestDeliveryFailureCoordinatorUsesCurrentFrozenWitness(t *testing.T) {
	plan, parent, child, _, _ := deliveryExportPlanFixture(t, nil)
	workflow, _ := v2input.WorkflowID(parent)
	export, err := plan.NewChildExportID(parent, child)
	if err != nil {
		t.Fatal(err)
	}
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Tenant: uuid.New(), Run: parent, Workflow: workflow, ParentTask: plan.TaskID(), Until: time.Now().Add(time.Minute), SlotCount: 1, Cursor: 1}
	for _, scenario := range []string{"failed", "pending", "foreign", "lost", "write_error", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			reader := &deliveryAssemblyMatrix{observation: delivery.ExportObservation{ExportID: export, SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA, RunState: "failed", ExportState: "failed"}}
			writer := &deliveryFailureWriter{fail: scenario == "write_error"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "pending":
				reader.observation.RunState = "running"
				reader.observation.ExportState = "queued"
			case "foreign":
				reader.observation.ExportID = uuid.New()
			case "lost":
				reader.failAt = 1
			case "cancel":
				cancel()
			}
			finalized, err := FinalizeCurrentDeliveryFailure(ctx, reader, writer, lease, plan)
			if scenario == "failed" {
				if err != nil || !finalized || writer.calls != 1 || writer.witness.ChildRun != child.RunID || writer.witness.ExportID != export {
					t.Fatal(finalized, err, writer)
				}
			} else if scenario == "pending" {
				if err != nil || finalized || writer.calls != 0 {
					t.Fatal(finalized, err, writer)
				}
			} else {
				if err == nil || finalized {
					t.Fatal(finalized, err)
				}
				if scenario != "write_error" && writer.calls != 0 {
					t.Fatal("invalid failure reached mutation")
				}
			}
		})
	}
}
