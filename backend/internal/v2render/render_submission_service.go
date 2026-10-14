package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"time"
)

type SQLRenderSubmissionService struct {
	Factory      SQLRenderBatchFactory
	Calibrations RenderCalibrationCatalog
	Timeout      time.Duration
}

// Queue owns ordered intent and serializes with autonomous input recovery.
// Pending replays use saved execution; completed replies need no source storage
// or calibration catalog. No IO is enclosed by an open SQL transaction.
func (s SQLRenderSubmissionService) Queue(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, key string, request RenderBatchSubmissionRequest) (RenderBatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return RenderBatchTicket{}, err
	}
	if who.SessionID == uuid.Nil || s.Timeout <= 0 || s.Timeout > 30*time.Minute {
		return RenderBatchTicket{}, v2input.ErrInvalid
	}
	request.Selection = append([]RenderSelectionItem(nil), request.Selection...)
	batch, requestSHA, err := CompileRenderSubmissionIdentity(tenant, who.UserID, project, key, request)
	if err != nil {
		return RenderBatchTicket{}, err
	}
	work, stop := context.WithTimeout(ctx, s.Timeout)
	defer stop()
	var ticket RenderBatchTicket
	done, err := withRenderBatchLock(work, s.Factory.Scheduler, tenant, batch, func(work context.Context) error {
		store := SQLRenderDraftStore{DB: s.Factory.Scheduler}
		saved, err := store.ProbeSubmissionKey(work, tenant, project, batch, who, key, requestSHA)
		if err != nil {
			return err
		}
		if saved.Completed {
			ticket, err = store.ActivateSubmission(work, tenant, batch, who, key, requestSHA)
			return err
		}
		if saved.CancelRequested {
			return v2input.ErrExpired
		}
		if !saved.Found {
			policy, policyErr := DefaultRenderPolicy()
			if policyErr != nil {
				return renderUnavailable()
			}
			if request.PolicySHA != policy.PolicySHA {
				return v2input.ErrInvalid
			}
			if s.Calibrations == nil {
				return renderUnavailable()
			}
			factory := s.Factory
			factory.SubmissionKey = key
			factory.SubmissionSHA = requestSHA
			if _, err := factory.PrepareCalibratedSelection(work, tenant, who, batch, project, request.SnapshotID, request.PolicySHA, request.Selection, s.Calibrations); err != nil {
				return err
			}
		} else {
			// Inputs may already be complete even while aggregate phase is still draft.
			ticket, err = store.ActivateSubmission(work, tenant, batch, who, key, requestSHA)
			if err == nil {
				return nil
			}
			var unavailable *domain.Error
			if !errors.As(err, &unavailable) || unavailable.Code != "RENDER_UNAVAILABLE" {
				return err
			}
			coordinator, err := s.Factory.ForProject(work, project)
			if err != nil {
				return err
			}
			if err := coordinator.RecoverInputs(work, tenant, batch, who); err != nil {
				return err
			}
		}
		ticket, err = store.ActivateSubmission(work, tenant, batch, who, key, requestSHA)
		return err
	})
	if err != nil {
		return RenderBatchTicket{}, err
	}
	if !done {
		return RenderBatchTicket{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return ticket, nil
}
