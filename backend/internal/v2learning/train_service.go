package v2learning

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type TrainingStart struct {
	TenantID, ActorID, AuthSessionID uuid.UUID
	ProfileID, ProjectID             uuid.UUID
	Key                              string
	Request                          TrainCreate
}

// TrainingAdmission owns scheduler-role reads, immutable artifact resolution,
// draft publication and graph seal. The API role never receives those grants.
type TrainingAdmission interface {
	Probe(context.Context, TrainingStart) (TrainingDraftProbe, error)
	Admit(context.Context, TrainingStart) (v2input.JobTicket, error)
}

func trainingError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrInsufficientTrainingData):
		return domain.NewError("INSUFFICIENT_DATA", 422)
	case errors.Is(err, ErrTrainingDraftConflict),
		errors.Is(err, ErrTrainingFreezeConflict),
		errors.Is(err, v2input.ErrConflict),
		errors.Is(err, v2input.ErrExpired):
		return domain.NewError("TRAINING_CONFLICT", 409)
	case errors.Is(err, ErrInvalidTrainingDraft),
		errors.Is(err, ErrInvalidTrainingManifest),
		errors.Is(err, ErrInvalidTrainingExample):
		return domain.NewError("VALIDATION_FAILED", 422)
	default:
		return correctionError(err)
	}
}

// TrainProfile checks project authorization before idempotency replay. A
// missing draft is compiled only after a current profile/CAS/consent check;
// the scheduler repeats these checks under its freeze transaction.
func (s ProfileService) TrainProfile(ctx context.Context, who auth.Identity,
	tenantID, profileID uuid.UUID, key string, req TrainCreate) (
	v2input.JobTicket, bool, error) {
	var empty v2input.JobTicket
	if s.DB == nil {
		return empty, false, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if profileID == uuid.Nil {
		return empty, false, domain.NewError("NOT_FOUND", 404)
	}
	if _, err := ValidateTrainCreate(req); err != nil {
		return empty, false, domain.NewError("VALIDATION_FAILED", 422)
	}
	if !idempotency.ValidKey(key) {
		return empty, false, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	var profile ProfileGet
	var projectID uuid.UUID
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, role domain.Role) error {
			if err := tx.QueryRow(ctx, `SELECT project_id FROM
			 photo.learning_profiles WHERE tenant_id=$1 AND id=$2`,
				tenantID, profileID).Scan(&projectID); err != nil {
				if err == pgx.ErrNoRows {
					return domain.NewError("NOT_FOUND", 404)
				}
				return err
			}
			if _, err := project.Authorize(ctx, tx, tenantID, projectID,
				who.UserID, role, domain.Editor, false); err != nil {
				return err
			}
			var err error
			profile, err = profileGet(ctx, tx, who, tenantID, profileID, role)
			return err
		})
	if err != nil {
		return empty, false, trainingError(err)
	}
	// An accepted immutable command is recoverable even when the optional
	// scheduler installation or inference provider has been withdrawn.
	if ticket, found, receiptErr := readAcceptedTrainingReceipt(ctx, s.DB, who, tenantID, profileID, key, req); receiptErr != nil {
		return empty, false, trainingError(receiptErr)
	} else if found { return ticket, true, nil }
	if s.Training == nil {
		return empty, false, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	start := TrainingStart{TenantID: tenantID, ActorID: who.UserID,
		AuthSessionID: who.SessionID, ProfileID: profileID,
		ProjectID: projectID, Key: key, Request: req}
	probe, err := s.Training.Probe(ctx, start)
	if err != nil {
		return empty, false, trainingError(err)
	}
	switch probe.Outcome {
	case "conflict", "expired":
		return empty, false, domain.NewError("TRAINING_CONFLICT", 409)
	case "replayed", "pending":
		ticket, err := s.Training.Admit(ctx, start)
		return ticket, probe.Outcome == "replayed", trainingError(err)
	case "missing":
		if profile.Revision != req.ExpectedRevision ||
			profile.Mode != req.Policy.Mode {
			return empty, false, domain.NewError("TRAINING_CONFLICT", 409)
		}
		if profile.ConsentState != "enabled" {
			return empty, false, domain.NewError("CONSENT_REQUIRED", 422)
		}
		ticket, err := s.Training.Admit(ctx, start)
		return ticket, false, trainingError(err)
	default:
		return empty, false, domain.NewError("INTERNAL_ERROR", 500)
	}
}
