package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

type deliveryActivationRow struct{ value any }

func (r deliveryActivationRow) Scan(dest ...any) error {
	switch p := dest[0].(type) {
	case *[]byte:
		*p = r.value.([]byte)
	case *bool:
		*p = r.value.(bool)
	case *int:
		*p = r.value.(int)
	default:
		panic("unexpected scan")
	}
	return nil
}

type deliveryActivationTx struct {
	pgx.Tx
	transportReady bool
	activatedReady bool
	args           []any
	payload        []byte
	calls          []string
	accepted       bool
}

func (tx *deliveryActivationTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	tx.calls = append(tx.calls, q)
	tx.args = args
	switch {
	case strings.Contains(q, "v30_delivery_continuation_activated"):
		return deliveryActivationRow{tx.activatedReady}
	case strings.Contains(q, "activate_v30_delivery_continuation"):
		return deliveryActivationRow{tx.accepted}
	case strings.Contains(q, "v30_delivery_continuation_transport_ready"):
		return deliveryActivationRow{tx.transportReady}
	case strings.Contains(q, "create_v30_delivery_continuation_task"):
		return deliveryActivationRow{tx.accepted}
	case strings.Contains(q, "v30_delivery_expansion_plan"):
		return deliveryActivationRow{tx.payload}
	case strings.Contains(q, "register_v2_task_input_batch"):
		var entries []v2input.Entry
		if json.Unmarshal(args[4].(json.RawMessage), &entries) != nil {
			panic("invalid batch")
		}
		return deliveryActivationRow{len(entries)}
	case strings.Contains(q, "seal_v2_task_inputs"):
		return deliveryActivationRow{true}
	case strings.Contains(q, "activate_v30_delivery_child"):
		return deliveryActivationRow{tx.accepted}
	default:
		panic("unexpected SQL")
	}
}

type deliveryTransportPublisher struct{}

func (deliveryTransportPublisher) Publish(_ context.Context, _ uuid.UUID, _ int, _ string, body []byte) (v2input.Ref, error) {
	return v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(body)}, nil
}

func TestDeliveryActivationUsesOwnedCursorAndDedicatedFinalCommit(t *testing.T) {
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
	graph, err := frozen.PublishInputs(context.Background(), deliveryTransportPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	graph.Open = func(context.Context) (v2input.Cursor, error) { panic("caller-replaced cursor used") }
	graph.Direct = nil
	workflow, _ := v2input.WorkflowID(parent)
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute), Tenant: uuid.New(), Run: parent, Workflow: workflow, Package: uuid.New(), ParentTask: plan.TaskID(), Cursor: 0, SlotCount: 1}
	var params delivery.PrepareParams
	if json.Unmarshal(plan.ParamsJCS(), &params) != nil {
		t.Fatal("parent params")
	}
	payload, err := canonical.Marshal(map[string]any{"proof": json.RawMessage(plan.ProofJCS()), "params": v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.ParamsJCS())}, "input": params.Input})
	if err != nil {
		t.Fatal(err)
	}
	tx := &deliveryActivationTx{payload: payload, accepted: true}
	if err := frozen.ActivateInTransaction(context.Background(), tx, lease, plan, child, uuid.New(), graph); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 4 || !strings.Contains(tx.calls[0], "expansion_plan") || !strings.Contains(tx.calls[1], "register_v2_task_input_batch") || !strings.Contains(tx.calls[2], "seal_v2_task_inputs") || !strings.Contains(tx.calls[3], "activate_v30_delivery_child") {
		t.Fatal("wrong transactional order", tx.calls)
	}
	tx.calls = nil
	tx.accepted = false
	if err := frozen.ActivateInTransaction(context.Background(), tx, lease, plan, child, uuid.New(), graph); err == nil {
		t.Fatal("activation refusal accepted")
	}
	exportID := uuid.New()
	receiptRaw, _ := canonical.Marshal(DeliveryChildReceipt{RunID: child.RunID, TaskID: node.ID, ExportID: exportID, Cursor: 1, Status: "failed", GraphSealed: false})
	receiptDB := &deliveryReceiptTx{raw: receiptRaw}
	receipt, err := frozen.RecoveryReceipt(context.Background(), receiptDB, lease, plan, child, exportID)
	if err != nil || receipt == nil || receipt.Status != "failed" || receipt.GraphSealed {
		t.Fatal("admitted failed child not acknowledged", err)
	}
	if len(receiptDB.args) != 6 || receiptDB.args[0] != lease.Expansion || receiptDB.args[1] != lease.Token || receiptDB.args[4] != exportID {
		t.Fatal("receipt fence binding")
	}
	for _, badReceipt := range [][]byte{nil, []byte("null"), []byte("{}"), append(append([]byte(nil), receiptRaw...), []byte(" {}")...)} {
		receiptDB.raw = badReceipt
		value, err := frozen.RecoveryReceipt(context.Background(), receiptDB, lease, plan, child, exportID)
		if len(badReceipt) == 0 || string(badReceipt) == "null" {
			if err != nil || value != nil {
				t.Fatal("unconfirmed receipt", err)
			}
		} else if err == nil {
			t.Fatal("malformed receipt accepted")
		}
	}
	receiptDB.raw = receiptRaw
	if _, err := frozen.RecoveryReceipt(context.Background(), receiptDB, lease, plan, child, uuid.New()); err == nil {
		t.Fatal("foreign export receipt accepted")
	}
	tx.calls = nil
	bad := graph
	bad.ID = uuid.New()
	if err := frozen.ActivateInTransaction(context.Background(), tx, lease, plan, child, uuid.New(), bad); err == nil || len(tx.calls) != 0 {
		t.Fatal("foreign graph reached SQL")
	}
}

type deliveryReceiptTx struct {
	raw  []byte
	args []any
}

func (tx *deliveryReceiptTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	tx.args = args
	return deliveryActivationRow{tx.raw}
}
