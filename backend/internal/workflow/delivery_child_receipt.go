package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type DeliveryChildReceipt struct {
	RunID       uuid.UUID `json:"run_id"`
	TaskID      uuid.UUID `json:"task_id"`
	ExportID    uuid.UUID `json:"export_id"`
	Cursor      int       `json:"cursor"`
	Status      string    `json:"status"`
	GraphSealed bool      `json:"graph_sealed"`
}

// RecoveryReceipt acknowledges persisted initial admission, never export success.
// nil means unconfirmed (including stale authority), not proof that no child
// exists. A caller must revalidate its lease before attempting draft recovery.
func (f FrozenDeliveryInitialTask) RecoveryReceipt(ctx context.Context, tx delivery.ProofTransaction, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, exportID uuid.UUID) (*DeliveryChildReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	draft, err := f.DraftTask()
	if err != nil {
		return nil, err
	}
	if tx == nil || exportID == uuid.Nil || lease.Expansion == uuid.Nil || lease.Token == uuid.Nil || lease.ParentTask != plan.TaskID() {
		return nil, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return nil, err
	}
	task, err := v2input.TaskID(workflow, draft.Operation, 0)
	if err != nil || task != draft.ID {
		return nil, v2input.ErrInvalid
	}
	_, request, err := plan.ExportRequest(lease.Run, child)
	if err != nil {
		return nil, err
	}
	taskRaw, err := canonical.Marshal(draft)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT photo.v30_delivery_child_receipt($1,$2,$3,$4,$5,$6::jsonb)`, lease.Expansion, lease.Token, child.SlotIndex, string(request), exportID, string(taskRaw)).Scan(&raw)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	if len(raw) > 4096 {
		return nil, v2input.ErrInvalid
	}
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil || len(shape) != 6 || (!bytes.Equal(bytes.TrimSpace(shape["graph_sealed"]), []byte("true")) && !bytes.Equal(bytes.TrimSpace(shape["graph_sealed"]), []byte("false"))) {
		return nil, v2input.ErrInvalid
	}
	var result DeliveryChildReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.RunID != child.RunID || result.TaskID != draft.ID || result.ExportID != exportID || result.Cursor <= child.SlotIndex || result.Cursor > lease.SlotCount {
		return nil, v2input.ErrInvalid
	}
	switch result.Status {
	case "queued", "running", "waiting_review", "completed", "failed", "cancelling", "cancelled":
	default:
		return nil, v2input.ErrInvalid
	}
	return &result, nil
}
