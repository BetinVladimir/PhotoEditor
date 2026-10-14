package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// The installed pending-slot reader requires app.tenant_id. Keep discovery in
// one short transaction and commit before any physical result inspection.
func readOrdinaryDeliveryPendingSlots(ctx context.Context, db delivery.MetadataTransactionBeginner, lease delivery.ExpansionLease, limit int) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil || !lease.Valid() || limit < 1 || limit > 100 {
		return nil, v2input.ErrInvalid
	}
	var slots []int
	err := assemblyTenantTransaction(ctx, db, lease, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		slots, err = ReadDeliveryPendingSlots(ctx, tx, lease, limit)
		return err
	})
	if err != nil {
		return nil, err
	}
	return slots, nil
}

func ReadDeliveryPendingSlots(ctx context.Context, db artifactjson.RowQuerier, lease delivery.ExpansionLease, limit int) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil || limit < 1 || limit > 100 || lease.Cursor < 0 || lease.Cursor > lease.SlotCount {
		return nil, v2input.ErrInvalid
	}
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT photo.v30_delivery_pending_child_slots($1,$2,$3)`, lease.Expansion, lease.Token, limit).Scan(&raw); err != nil {
		return nil, err
	}
	return parseDeliveryPendingSlots(raw, lease.Cursor, limit)
}
func parseDeliveryPendingSlots(raw []byte, cursor, limit int) ([]int, error) {
	var slots []int
	if len(raw) > 4096 || json.Unmarshal(raw, &slots) != nil || slots == nil || len(slots) > limit {
		return nil, v2input.ErrInvalid
	}
	last := -1
	for _, slot := range slots {
		if slot <= last || slot < 0 || slot >= cursor {
			return nil, v2input.ErrInvalid
		}
		last = slot
	}
	return slots, nil
}

// ReadSavedDeliveryContinuationParams recovers exact compiled Params before
// attempting publication on a child whose graph may already be sealed again.
// nil remains unconfirmed; publication/activation must check current authority.
func ReadSavedDeliveryContinuationParams(ctx context.Context, db artifactjson.RowQuerier, lease delivery.ExpansionLease, child delivery.ExportChildIntent, node CompiledDownloadNode) (*v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil || (node.Operation != "derivative.sharpen" && node.Operation != "export.encode") {
		return nil, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return nil, err
	}
	expected, err := v2input.TaskID(workflow, node.Operation, 0)
	if err != nil || expected != node.ID {
		return nil, v2input.ErrInvalid
	}
	sha := renderDerivativeBytesSHA(node.Params)
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT photo.v30_delivery_saved_continuation_params($1,$2,$3,$4,$5)`, lease.Expansion, lease.Token, child.SlotIndex, node.Operation, sha).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	if len(raw) > 512 {
		return nil, v2input.ErrInvalid
	}
	var ref v2input.Ref
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&ref) != nil || decoder.Decode(new(any)) != io.EOF || ref.ArtifactID == uuid.Nil || ref.SHA256 != sha {
		return nil, v2input.ErrInvalid
	}
	return &ref, nil
}
