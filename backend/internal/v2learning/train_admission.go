package v2learning

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type TrainingSourceSnapshot struct {
	Context       ManifestContext
	Candidates    []TrainingCandidate
	Evidence      []TrainingCaseEvidence
	Excluded      []ExcludedTrainingCandidate
	ReservedBytes int64
}

// TrainingSource resolves current candidate inventory only for a missing
// draft. Restore recompiles bytes from immutable document refs pinned in the
// pending recovery manifest; it must not choose a new candidate inventory.
type TrainingSource interface {
	Resolve(context.Context, TrainingStart) (TrainingSourceSnapshot, error)
	Restore(context.Context, TrainingStart, TrainingDraftPrepared) (
		map[uuid.UUID]CompiledTrainingRecipes, error)
}

type SQLTrainingAdmission struct {
	DB      *pgxpool.Pool // photo_scheduler
	Runtime TrainingFitRuntime
	Source  TrainingSource
}

func (a SQLTrainingAdmission) Probe(ctx context.Context,
	start TrainingStart) (TrainingDraftProbe, error) {
	if a.DB == nil || start.TenantID == uuid.Nil ||
		start.ActorID == uuid.Nil || start.AuthSessionID == uuid.Nil ||
		start.ProfileID == uuid.Nil || start.ProjectID == uuid.Nil {
		return TrainingDraftProbe{}, ErrInvalidTrainingDraft
	}
	return ProbeTrainingDraft(ctx, a.DB, start.TenantID, start.ActorID,
		start.AuthSessionID, start.Key, start.Request)
}

func (a SQLTrainingAdmission) Admit(ctx context.Context,
	start TrainingStart) (v2input.JobTicket, error) {
	var empty v2input.JobTicket
	if a.Source == nil || a.Runtime.DB == nil || a.Runtime.Objects == nil {
		return empty, ErrInvalidTrainingDraft
	}
	probe, err := a.Probe(ctx, start)
	if err != nil {
		return empty, err
	}
	requestSHA, err := canonical.Hash(start.Request)
	if err != nil {
		return empty, err
	}
	ticket := func() (v2input.JobTicket, error) {
		return (v2input.SQLAdmissionStore{DB: a.DB}).Ticket(ctx,
			start.TenantID, start.ActorID, start.Key, requestSHA)
	}
	if probe.Outcome == "replayed" {
		return ticket()
	}
	if probe.Outcome == "conflict" || probe.Outcome == "expired" {
		return empty, ErrTrainingDraftConflict
	}
	var draft TrainingDraftPrepared
	var frozen map[uuid.UUID]CompiledTrainingRecipes
	if probe.Outcome == "pending" {
		draft, err = ResumeTrainingDraft(start.TenantID, start.ActorID,
			start.AuthSessionID, start.ProjectID, start.ProfileID,
			start.Key, start.Request, probe.Recovery)
		if err != nil {
			return empty, err
		}
		frozen, err = a.Source.Restore(ctx, start, draft)
	} else if probe.Outcome == "missing" {
		var source TrainingSourceSnapshot
		source, err = a.Source.Resolve(ctx, start)
		if err != nil {
			return empty, err
		}
		var compiled TrainingCompilation
		compiled, err = CompileTrainingDraftWithExclusions(start.TenantID, start.ActorID,
			start.AuthSessionID, start.ProjectID, start.ProfileID, start.Key,
			source.ReservedBytes, start.Request, source.Context,
			source.Candidates, source.Evidence, source.Excluded)
		draft, frozen = compiled.Draft, compiled.FrozenRecipes
	} else {
		return empty, ErrInvalidTrainingDraft
	}
	if err != nil {
		return empty, err
	}
	orchestrator, err := a.Runtime.Orchestrator(draft, frozen)
	if err != nil {
		return empty, err
	}
	if _, err = orchestrator.Admit(ctx, draft, start.Request.Policy); err != nil {
		return empty, err
	}
	return ticket()
}

var _ TrainingAdmission = SQLTrainingAdmission{}
