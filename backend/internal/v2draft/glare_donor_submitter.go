package v2draft

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type GlareDonorDraftBeginner interface {
	Begin(context.Context, GlareDonorDraftBeginRequest) (GlareDonorDraftIdentity, error)
}

// GlareDonorRestorationSubmitter owns the order of draft creation and graph
// publication. The request's Actor, Pin, Snapshot and reservation must be
// supplied by an authenticated server loader, not copied from HTTP JSON.
type GlareDonorRestorationSubmitter struct {
	Beginner    GlareDonorDraftBeginner
	Coordinator GlareDonorRestorationCoordinator
}

func (s GlareDonorRestorationSubmitter) Submit(ctx context.Context,
	r GlareDonorDraftBeginRequest, baseProfile []byte) (GlareDonorRestorationSubmission, error) {
	var zero GlareDonorRestorationSubmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Beginner == nil || s.Coordinator.DB == nil || r.Tenant == uuid.Nil ||
		r.Actor.UserID == uuid.Nil || r.Actor.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	run, err := v2input.RunID(r.Tenant, r.Actor.UserID, r.Key)
	if err != nil {
		return zero, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	profile, err := v2portrait.CompilePinnedRestorationProfile("portrait.glare",
		r.Proposal, baseProfile, r.Pin)
	if err != nil {
		return zero, err
	}
	store := SQLRestorationDescriptorStore{DB: s.Coordinator.DB}
	readTicket := func() (RestorationAdmissionTicket, error) {
		return store.ReadAdmissionTicketForWorkflow(ctx, r.Actor, r.Tenant,
			r.Project, r.Shoot, run, workflow, r.Snapshot, r.Ordinal, r.Proposal, profile,
			r.SourceObservation, r.DonorObservation)
	}
	if ticket, readErr := readTicket(); readErr == nil {
		return GlareDonorRestorationSubmission{TaskID: ticket.TaskID,
			Ticket: ticket, Recovered: true}, nil
	} else if !errors.Is(readErr, v2input.ErrExpired) {
		return zero, readErr
	}
	identity, err := s.Beginner.Begin(ctx, r)
	if err != nil {
		if ticket, readErr := readTicket(); readErr == nil {
			return GlareDonorRestorationSubmission{TaskID: ticket.TaskID,
				Ticket: ticket, Recovered: true}, nil
		}
		return zero, err
	}
	if identity.Run != run || identity.Workflow != workflow ||
		identity.BaseVersion == uuid.Nil || identity.DonorVersion == uuid.Nil ||
		identity.BaseVersion == identity.DonorVersion ||
		(s.Coordinator.CatalogProject != uuid.Nil &&
			(s.Coordinator.CatalogProject != r.Project ||
				s.Coordinator.BaseAssetVersion != identity.BaseVersion ||
				s.Coordinator.DonorAssetVersion != identity.DonorVersion)) {
		return zero, v2input.ErrInvalid
	}
	return s.Coordinator.Admit(ctx, r.Actor, r.Tenant, r.Project, r.Shoot,
		run, workflow, r.Snapshot, r.Ordinal, r.Proposal, baseProfile, r.Pin,
		r.SourceObservation, r.DonorObservation)
}
