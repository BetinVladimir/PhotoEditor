package v2draft

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type ReflectionDraftBeginner interface {
	BeginReflection(context.Context, ReflectionDraftBeginRequest) (ReflectionDraftIdentity, error)
}

type ReflectionRestorationSubmitter struct {
	Beginner    ReflectionDraftBeginner
	Coordinator ReflectionRestorationCoordinator
}

type ReflectionSubmissionResult struct {
	Ticket    RestorationAdmissionTicket
	Recovered bool
}

// Submit recovers a committed graph before trying to recreate a draft. An
// ambiguous commit is acknowledged only by a fresh current-authority receipt.
func (s ReflectionRestorationSubmitter) Submit(ctx context.Context,
	r ReflectionDraftBeginRequest, baseProfile []byte) (ReflectionSubmissionResult, error) {
	var zero ReflectionSubmissionResult
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Beginner == nil || s.Coordinator.DB == nil || r.Tenant == uuid.Nil ||
		r.Project == uuid.Nil || r.Actor.UserID == uuid.Nil ||
		r.Actor.SessionID == uuid.Nil || r.Pin.Operation != "image.reflection" {
		return zero, v2input.ErrInvalid
	}
	intentRaw, err := canonical.Marshal(r.Intent)
	if err != nil {
		return zero, err
	}
	intent, err := DecodeReflectionClientIntent(intentRaw)
	if err != nil {
		return zero, err
	}
	run, err := v2input.RunID(r.Tenant, r.Actor.UserID, r.Key)
	if err != nil {
		return zero, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	expectedTask, err := v2input.TaskID(workflow, "image.reflection", intent.Ordinal)
	if err != nil {
		return zero, err
	}
	profile, err := v2portrait.CompilePinnedRestorationProfile("image.reflection",
		intent.Proposal, baseProfile, r.Pin)
	if err != nil {
		return zero, err
	}
	var selected struct {
		Base struct {
			AssetVersionID uuid.UUID `json:"asset_version_id"`
		} `json:"base"`
		ModelLockSHA string      `json:"model_lock_sha"`
		Calibration  v2input.Ref `json:"calibration"`
	}
	if json.Unmarshal(intent.Proposal, &selected) != nil ||
		selected.ModelLockSHA != r.Pin.ModelLockSHA ||
		selected.Calibration != r.Pin.Calibration ||
		selected.Base.AssetVersionID == uuid.Nil ||
		s.Coordinator.CatalogProject != r.Project ||
		s.Coordinator.BaseAssetVersion != selected.Base.AssetVersionID {
		return zero, v2input.ErrInvalid
	}
	store := SQLRestorationDescriptorStore{DB: s.Coordinator.DB}
	readTicket := func() (RestorationAdmissionTicket, error) {
		return store.ReadReflectionAdmissionTicket(ctx, r.Actor, r.Tenant, r.Project,
			run, workflow, intent.Snapshot, intent.Ordinal, intent.Proposal, profile,
			intent.TargetPixel, intent.ProtectedPixel)
	}
	if ticket, readErr := readTicket(); readErr == nil {
		return ReflectionSubmissionResult{Ticket: ticket, Recovered: true}, nil
	} else if !errors.Is(readErr, v2input.ErrExpired) {
		return zero, readErr
	}
	identity, err := s.Beginner.BeginReflection(ctx, r)
	if err != nil {
		if ticket, readErr := readTicket(); readErr == nil {
			return ReflectionSubmissionResult{Ticket: ticket, Recovered: true}, nil
		}
		return zero, err
	}
	if identity.Run != run || identity.Workflow != workflow ||
		identity.BaseVersion != selected.Base.AssetVersionID {
		return zero, v2input.ErrInvalid
	}
	admitted, err := s.Coordinator.Admit(ctx, r.Actor, r.Tenant, r.Project,
		run, workflow, intent.Snapshot, intent.Ordinal, intent.Proposal,
		baseProfile, r.Pin)
	if err != nil {
		if ticket, readErr := readTicket(); readErr == nil {
			return ReflectionSubmissionResult{Ticket: ticket, Recovered: true}, nil
		}
		return zero, err
	}
	ticket, err := readTicket()
	if err != nil {
		return zero, err
	}
	if ticket.TaskID != expectedTask || admitted.TaskID != expectedTask {
		return zero, v2input.ErrInvalid
	}
	return ReflectionSubmissionResult{Ticket: ticket}, nil
}
