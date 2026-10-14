package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
	"time"
)

type deliveryCoordinatorStore struct {
	calls    *[]string
	fail     string
	receipt  *DeliveryChildReceipt
	reserved int64
	id       uuid.UUID
}

func (s *deliveryCoordinatorStore) step(name string) error {
	*s.calls = append(*s.calls, name)
	if s.fail == name {
		return errors.New(name)
	}
	return nil
}
func (s *deliveryCoordinatorStore) Receipt(_ context.Context, _ FrozenDeliveryInitialTask, _ delivery.ExpansionLease, _ delivery.PreparePlan, _ delivery.ExportChildIntent, id uuid.UUID) (*DeliveryChildReceipt, error) {
	s.id = id
	return s.receipt, s.step("receipt")
}
func (s *deliveryCoordinatorStore) Draft(_ context.Context, _ FrozenDeliveryInitialTask, _ delivery.ExpansionLease, _ delivery.PreparePlan, _ delivery.ExportChildIntent, id uuid.UUID, reserved int64) error {
	if id != s.id {
		panic("identity changed")
	}
	s.reserved = reserved
	return s.step("draft")
}
func (s *deliveryCoordinatorStore) Activate(_ context.Context, _ FrozenDeliveryInitialTask, _ delivery.ExpansionLease, _ delivery.PreparePlan, _ delivery.ExportChildIntent, id uuid.UUID, _ v2input.GraphTask) error {
	if id != s.id {
		panic("identity changed")
	}
	return s.step("activate")
}

type deliveryCoordinatorPublisher struct {
	calls *[]string
	fail  bool
}

func (p deliveryCoordinatorPublisher) Publish(_ context.Context, _ uuid.UUID, _ int, _ string, body []byte) (v2input.Ref, error) {
	*p.calls = append(*p.calls, "publish")
	if p.fail {
		return v2input.Ref{}, errors.New("publish")
	}
	return v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(body)}, nil
}

func TestDeliveryChildCoordinatorOrderRecoveryAndFailures(t *testing.T) {
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
	entries := []v2input.Entry{{Ref: child.Binding.Source, Contract: "WorkingImage"}, {Ref: child.Recipe, Contract: "EditingRecipeV3"}, {Ref: child.Preset.Config, Contract: "ExportPresetConfig"}, {Ref: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, Contract: "NpyArray"}}
	if image.Alpha != nil {
		entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, Contract: "NpyArray"})
	}
	frozen, err := FreezeDeliveryInitialTask(plan, parent, child, raw, base.TargetICCSHA256, registry, v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(node.Params)}, entries, DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	workflow, _ := v2input.WorkflowID(parent)
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute), Tenant: uuid.New(), Run: parent, Workflow: workflow, Package: uuid.New(), ParentTask: plan.TaskID(), Cursor: 0, SlotCount: 1}
	id, _ := plan.NewChildExportID(parent, child)
	for _, scenario := range []string{"success", "recovery", "foreign_receipt", "receipt", "draft", "publish", "activate", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			calls := []string{}
			store := &deliveryCoordinatorStore{calls: &calls, fail: scenario}
			publisher := deliveryCoordinatorPublisher{calls: &calls, fail: scenario == "publish"}
			if scenario == "recovery" || scenario == "foreign_receipt" {
				store.receipt = &DeliveryChildReceipt{RunID: child.RunID, TaskID: node.ID, ExportID: id, Cursor: 1, Status: "failed"}
			}
			if scenario == "foreign_receipt" {
				store.receipt.ExportID = uuid.New()
			}
			ctx := context.Background()
			if scenario == "cancelled" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			result, err := frozen.AdmitNewChild(ctx, store, publisher, lease, plan, child)
			if scenario == "success" {
				if err != nil || result.ExportID != id || result.NextCursor != 1 || result.Recovered {
					t.Fatal(result, err)
				}
				if len(calls) < 4 || calls[0] != "receipt" || calls[1] != "draft" || calls[len(calls)-1] != "activate" {
					t.Fatal(calls)
				}
				for _, call := range calls[2 : len(calls)-1] {
					if call != "publish" {
						t.Fatal(calls)
					}
				}
				estimate, _ := ParseResourceEstimate(frozen.draft.ExecutionProfile)
				config, _, _ := plan.ChildConfig(parent, child, id)
				if store.reserved != estimate.OutputBytes+int64(len(config)+len(frozen.closure.ParamsJCS)) {
					t.Fatal("reservation mismatch")
				}
			} else if scenario == "recovery" {
				if err != nil || !result.Recovered || !reflect.DeepEqual(calls, []string{"receipt"}) {
					t.Fatal(result, err, calls)
				}
			} else {
				if err == nil || result.RunID != uuid.Nil {
					t.Fatal("failure accepted", scenario)
				}
				switch scenario {
				case "cancelled":
					if len(calls) != 0 {
						t.Fatal(calls)
					}
				case "receipt", "foreign_receipt":
					if !reflect.DeepEqual(calls, []string{"receipt"}) {
						t.Fatal(calls)
					}
				case "draft":
					if !reflect.DeepEqual(calls, []string{"receipt", "draft"}) {
						t.Fatal(calls)
					}
				case "publish":
					if calls[len(calls)-1] != "publish" {
						t.Fatal(calls)
					}
				case "activate":
					if calls[len(calls)-1] != "activate" {
						t.Fatal(calls)
					}
				}
			}
		})
	}
}
