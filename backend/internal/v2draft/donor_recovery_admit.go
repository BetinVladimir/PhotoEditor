package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// RecoverPublishAndAdmit resumes a pre-admission intent through original-evidence
// reload, replay-stable Params publication, exact transport registration and
// current-authority graph sealing. Each SQL transaction ends before object IO.
// The returned ticket is read under fresh authority after committed admission.
// On an unknown commit callers must first use ReadQueuedTicket; this method must
// not be used to blindly replay a run that may already have been admitted.
func (s SQLDonorParamsPublisher) RecoverPublishAndAdmit(ctx context.Context, who auth.Identity,
	tenant, project, shoot, run, workflow uuid.UUID, loader DonorRecoveryLoader,
	manual DonorManualProducer, documents v2input.DocumentPublisher) (DonorQueuedTicket, error) {
	var zero DonorQueuedTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Publisher == nil || documents == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		tenant == uuid.Nil || project == uuid.Nil || shoot == uuid.Nil || run == uuid.Nil || workflow == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if err := loader.validateConfiguration(); err != nil {
		return zero, err
	}
	manual.RootRaw = appendOptionalBytes(manual.RootRaw)
	manual.ParamsRaw = appendOptionalBytes(manual.ParamsRaw)
	manual.VectorsRaw = appendOptionalBytes(manual.VectorsRaw)
	seed, err := s.ReadRecoverySeed(ctx, who, tenant, run, workflow)
	if err != nil {
		return zero, err
	}
	loaded, prepared, err := loader.Reload(ctx, who, tenant, project, workflow, seed, manual)
	if err != nil {
		return zero, err
	}
	published, err := s.Publish(ctx, who, tenant, run, workflow, seed.Ordinal(), loaded, prepared)
	if err != nil {
		return zero, err
	}
	frozen, err := FreezeQualifiedDonorTask(ctx, workflow, seed.Ordinal(), loaded, prepared, published)
	if err != nil {
		return zero, err
	}
	store := SQLDonorDescriptorStore{DB: s.DB}
	if _, err = store.PublishAndRegisterTransport(ctx, who, tenant, run, frozen, documents); err != nil {
		return zero, err
	}
	if err = store.AdmitRegistered(ctx, who, tenant, project, shoot, run, workflow, seed.Ordinal(), frozen, loaded, prepared); err != nil {
		return zero, err
	}
	ticket, err := store.ReadQueuedTicketForWorkflow(ctx, who, tenant, run, workflow, seed.Ordinal())
	if err != nil {
		return zero, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	if ticket.WorkflowID != workflow || ticket.TaskID != draft.ID {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ticket, nil
}
