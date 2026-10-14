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

// RestorationAdmissionTicket records historical graph admission. It cannot
// dispatch a task or assert its present runtime/result state.
type RestorationAdmissionTicket struct {
	RunID      uuid.UUID `json:"run_id"`
	WorkflowID uuid.UUID `json:"workflow_id"`
	TaskID     uuid.UUID `json:"task_id"`
	State      string    `json:"state"`
}

// ReadAdmissionTicket is a DB-only unknown-commit recovery path. A missing
// answer does not distinguish no commit from revoked current authority.
func (s SQLRestorationDescriptorStore) ReadAdmissionTicket(ctx context.Context,
	who auth.Identity, tenant, project, shoot, run, snapshot uuid.UUID, proposal, profile []byte,
	sourceObservation, donorObservation uuid.UUID) (RestorationAdmissionTicket, error) {
	var zero RestorationAdmissionTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || tenant == uuid.Nil || project == uuid.Nil ||
		shoot == uuid.Nil || run == uuid.Nil || snapshot == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		sourceObservation == uuid.Nil || donorObservation == uuid.Nil ||
		sourceObservation == donorObservation {
		return zero, v2input.ErrInvalid
	}
	for _, raw := range [][]byte{proposal, profile} {
		if len(raw) < 2 || len(raw) > 65536 {
			return zero, v2input.ErrInvalid
		}
		canonicalRaw, err := canonical.Bytes(raw)
		if err != nil || !bytes.Equal(raw, canonicalRaw) {
			return zero, v2input.ErrInvalid
		}
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
		"SELECT photo.read_v28_restoration_admission_ticket($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)",
		tenant, run, who.UserID, who.SessionID, project, shoot, snapshot,
		string(proposal), string(profile),
		sourceObservation, donorObservation).Scan(&raw); err != nil {
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
		ticket.WorkflowID == uuid.Nil || ticket.TaskID == uuid.Nil || ticket.State != "queued" {
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

// ReadAdmissionTicketForWorkflow compares the SQL ticket with the caller's
// original deterministic task; it never authorizes a new submission on miss.
func (s SQLRestorationDescriptorStore) ReadAdmissionTicketForWorkflow(ctx context.Context,
	who auth.Identity, tenant, project, shoot, run, workflow, snapshot uuid.UUID, ordinal int,
	proposal, profile []byte, sourceObservation, donorObservation uuid.UUID) (RestorationAdmissionTicket, error) {
	var zero RestorationAdmissionTicket
	expected, err := v2input.TaskID(workflow, "portrait.glare", ordinal)
	if err != nil {
		return zero, err
	}
	ticket, err := s.ReadAdmissionTicket(ctx, who, tenant, project, shoot, run, snapshot,
		proposal, profile,
		sourceObservation, donorObservation)
	if err != nil {
		return zero, err
	}
	if ticket.WorkflowID != workflow || ticket.TaskID != expected {
		return zero, v2input.ErrInvalid
	}
	return ticket, nil
}
