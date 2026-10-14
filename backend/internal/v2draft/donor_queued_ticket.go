package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// DonorQueuedTicket acknowledges historical committed admission. State is the
// admission state, not the current processing status. Read runtime status through
// the normal authorized run endpoint; never dispatch work from this ticket.
type DonorQueuedTicket struct {
	RunID      uuid.UUID `json:"run_id"`
	WorkflowID uuid.UUID `json:"workflow_id"`
	TaskID     uuid.UUID `json:"task_id"`
	State      string    `json:"state"`
}

// ReadQueuedTicket resolves an unknown commit through a fresh transaction and
// current receipt authority, without object IO, re-registration or activation.
func (s SQLDonorDescriptorStore) ReadQueuedTicket(ctx context.Context, who auth.Identity, tenant, run uuid.UUID) (DonorQueuedTicket, error) {
	var zero DonorQueuedTicket
	raw, err := s.ReadQueuedReceipt(ctx, who, tenant, run)
	if err != nil {
		return zero, err
	}
	var ticket DonorQueuedTicket
	if json.Unmarshal(raw, &ticket) != nil || ticket.RunID != run || ticket.WorkflowID == uuid.Nil || ticket.TaskID == uuid.Nil || ticket.State != "queued" {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return ticket, nil
}

// ReadQueuedTicketForWorkflow is the DB-only unknown-commit recovery entry point.
// It requires the caller's original workflow/ordinal and rejects a receipt for
// another deterministic task. ErrExpired does not distinguish absence from
// revoked authority and must never by itself trigger new publication/admission.
func (s SQLDonorDescriptorStore) ReadQueuedTicketForWorkflow(ctx context.Context, who auth.Identity,
	tenant, run, workflow uuid.UUID, ordinal int) (DonorQueuedTicket, error) {
	var zero DonorQueuedTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	expected, err := v2input.TaskID(workflow, "portrait.open_eyes", ordinal)
	if err != nil {
		return zero, err
	}
	ticket, err := s.ReadQueuedTicket(ctx, who, tenant, run)
	if err != nil {
		return zero, err
	}
	if ticket.WorkflowID != workflow || ticket.TaskID != expected {
		return zero, v2input.ErrInvalid
	}
	return ticket, nil
}
