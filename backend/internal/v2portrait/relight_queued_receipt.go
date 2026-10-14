package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

// ReadQueuedReceipt recovers the historical committed admission document under
// current SQL source/qualification checks. It never reactivates work and
// does not represent its current processing state. Only owned committed bytes
// are returned; callers must not use a receipt as worker execution authority.
func (s RelightBootstrapCoordinator) ReadRelightQueuedReceipt(ctx context.Context, who auth.Identity, tenant, run uuid.UUID) ([]byte, error) {
	return s.readRelightQueuedReceipt(ctx, who, tenant, run, nil)
}

type relightReceiptBinding struct {
	project      uuid.UUID
	request, pin []byte
}

func (s RelightBootstrapCoordinator) readRelightQueuedReceipt(ctx context.Context, who auth.Identity, tenant, run uuid.UUID, binding *relightReceiptBinding) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || run == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if tx == nil {
		return nil, v2input.ErrInvalid
	}
	defer tx.Rollback(context.Background())
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return nil, err
	}
	if role != "photo_scheduler" {
		return nil, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return nil, err
	}
	var raw []byte
	if binding == nil {
		err = tx.QueryRow(ctx, "SELECT photo.read_v28_relight_queued_receipt($1,$2,$3,$4)", tenant, run, who.UserID, who.SessionID).Scan(&raw)
	} else {
		err = tx.QueryRow(ctx, "SELECT photo.read_v28_relight_request_receipt($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb)", tenant, binding.project, run, who.UserID, who.SessionID, string(binding.request), string(binding.pin)).Scan(&raw)
	}
	if err != nil {
		return nil, err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return nil, v2input.ErrExpired
	}
	body, err := canonical.Bytes(raw)
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || len(fields) != 10 {
		return nil, v2input.ErrInvalid
	}
	for _, key := range []string{"schema_version", "run_id", "workflow_id", "task_id", "actor_id", "auth_session_id", "state", "descriptor_sha", "transport", "qualification"} {
		if _, exists := fields[key]; !exists {
			return nil, v2input.ErrInvalid
		}
	}
	var schema int
	var state string
	var actor, session, runID, workflow, task uuid.UUID
	var digest string
	if json.Unmarshal(fields["schema_version"], &schema) != nil || schema != 1 || json.Unmarshal(fields["state"], &state) != nil || state != "queued" || json.Unmarshal(fields["actor_id"], &actor) != nil || actor != who.UserID || json.Unmarshal(fields["auth_session_id"], &session) != nil || session != who.SessionID || json.Unmarshal(fields["run_id"], &runID) != nil || runID != run || json.Unmarshal(fields["workflow_id"], &workflow) != nil || workflow == uuid.Nil || json.Unmarshal(fields["task_id"], &task) != nil || task == uuid.Nil || json.Unmarshal(fields["descriptor_sha"], &digest) != nil || !validRelightTransportRef(v2input.Ref{ArtifactID: run, SHA256: digest}) {
		return nil, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return append([]byte(nil), body...), nil
}
