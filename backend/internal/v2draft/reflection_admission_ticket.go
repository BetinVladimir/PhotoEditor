package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ReadReflectionAdmissionTicket recovers only an exact committed graph. A
// null SQL answer deliberately gives no permission to claim a queued task.
func (s SQLRestorationDescriptorStore) ReadReflectionAdmissionTicket(ctx context.Context,
	who auth.Identity, tenant, project, run, workflow, snapshot uuid.UUID,
	ordinal int, proposal, profile []byte, targetPixel, protectedPixel v2input.Ref) (
	RestorationAdmissionTicket, error) {
	var zero RestorationAdmissionTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || tenant == uuid.Nil || project == uuid.Nil ||
		run == uuid.Nil || workflow == uuid.Nil || snapshot == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	expected, err := v2input.TaskID(workflow, "image.reflection", ordinal)
	if err != nil {
		return zero, err
	}
	for _, raw := range [][]byte{proposal, profile} {
		if len(raw) < 2 || len(raw) > 65536 {
			return zero, v2input.ErrInvalid
		}
		normal, err := canonical.Bytes(raw)
		if err != nil || !bytes.Equal(normal, raw) {
			return zero, v2input.ErrInvalid
		}
	}
	for _, ref := range []v2input.Ref{targetPixel, protectedPixel} {
		if !isDocumentRef(ref) {
			return zero, v2input.ErrInvalid
		}
	}
	if targetPixel.ArtifactID == protectedPixel.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	targetRaw, err := canonical.Marshal(targetPixel)
	if err != nil {
		return zero, err
	}
	protectedRaw, err := canonical.Marshal(protectedPixel)
	if err != nil {
		return zero, err
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer rollbackDonorRecovery(bounded, tx)
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	var raw []byte
	if err = tx.QueryRow(bounded,
		"SELECT photo.read_v28_reflection_admission_ticket($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb)",
		tenant, run, who.UserID, who.SessionID, project, snapshot,
		string(proposal), string(profile), string(targetRaw), string(protectedRaw)).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 4096 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var ticket RestorationAdmissionTicket
	if json.Unmarshal(raw, &ticket) != nil || ticket.RunID != run ||
		ticket.WorkflowID != workflow || ticket.TaskID != expected ||
		ticket.State != "queued" {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(ticket)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	if err = bounded.Err(); err != nil {
		return zero, err
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return ticket, nil
}
