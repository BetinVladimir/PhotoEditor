package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"time"
)

// Recovers only the original person child admission, never parent/result adoption.
type SQLNativePersonAdmissionRecovery struct {
	SQLNativeInitialPipelineRecovery
}

func (j SQLNativePersonAdmissionRecovery) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.Preparer.Scheduler == nil || j.Preparer.Scheduler.Config().MaxConns < 2 || j.CatalogForReference == nil || j.Preparer.Reader.DB == nil || j.Preparer.Reader.Objects == nil || j.Preparer.Reader.ProjectID == uuid.Nil || j.Preparer.Documents.DB == nil || j.Preparer.Documents.Objects == nil || j.Preparer.Documents.Bucket != j.Preparer.Reader.Bucket || j.Preparer.TransportDocuments == nil || j.Timeout <= 0 || j.Timeout > 29*time.Minute {
		return v2input.ErrInvalid
	}
	_, err := createPersonBudget(j.Preparer.Configuration, j.Preparer.ReservedBytes, v2qualification.Metrics{})
	return err
}
func (j SQLNativePersonAdmissionRecovery) Recover(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID) (bool, bool, error) {
	if err := j.validate(ctx); err != nil {
		return false, false, err
	}
	work, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()
	admitted := false
	locked, err := withNativeManualParentLock(work, j.Preparer.Scheduler, tenantID, parent, func(work context.Context) error {
		capacity := ownTransferPreparation(j.Preparer.Configuration)
		anchor, err := j.Preparer.Reader.loadPersonPreparationAnchor(work, who, tenantID, parent, capacity.Color, capacity.MetadataBytes)
		if err != nil {
			return err
		}
		catalog, err := resolveNativeParentRecoveryCatalog(work, who, anchor, j.Preparer.Reader.ProjectID, nil, j.CatalogForReference)
		if err != nil {
			return err
		}
		plan, err := j.Preparer.Reader.restorePersonDraftPlan(work, catalog, who, anchor)
		if err != nil {
			return err
		}
		state, err := j.Preparer.Reader.readNativeInputSavedStageState(work, who, anchor.Request, anchor.Published.Ref, anchor.Snapshot, "mask.person")
		if err != nil {
			return err
		}
		if !state.sealed {
			run, _, err := personPreparationIDs(parent)
			if err != nil {
				return err
			}
			publisher := v2input.ScopedPublisher{Documents: j.Preparer.TransportDocuments, Tenant: tenantID, Run: run}
			if _, err := j.Preparer.Reader.persistPersonPreparationInputs(work, j.Preparer.Scheduler, publisher, who, plan, capacity.Color); err != nil {
				return err
			}
		}
		_, err = j.Preparer.Reader.resumePersonPreparation(work, j.Preparer.Scheduler, who, plan)
		admitted = err == nil
		return err
	})
	if err != nil {
		return false, locked, err
	}
	return admitted, locked, nil
}

func (j SQLNativePersonAdmissionRecovery) ProcessOne(ctx context.Context) (bool, error) {
	if err := j.validate(ctx); err != nil {
		return false, err
	}
	var tenantID, person, parent, token uuid.UUID
	var who auth.Identity
	leaseSeconds := int((j.Timeout+time.Second-1)/time.Second) + 10
	claim, stop := context.WithTimeout(ctx, 5*time.Second)
	err := j.Preparer.Scheduler.QueryRow(claim, "SELECT tenant_id,person_run_id,parent_run_id,actor_id,session_id,auth_epoch,lease_token FROM photo.claim_v27_person_admission($1,$2)", j.Preparer.Reader.ProjectID, leaseSeconds).Scan(&tenantID, &person, &parent, &who.UserID, &who.SessionID, &who.Epoch, &token)
	stop()
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if tenantID == uuid.Nil || person == uuid.Nil || parent == uuid.Nil || token == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return true, v2input.ErrConflict
	}
	admitted, locked, attemptErr := j.Recover(ctx, who, tenantID, parent)
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	outcome := "waiting"
	if locked || attemptErr != nil {
		if attemptErr != nil {
			outcome = "retry"
		} else if admitted {
			outcome = "admitted"
		}
	}
	finish, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var recorded bool
	recordErr := j.Preparer.Scheduler.QueryRow(finish, "SELECT photo.record_v27_person_admission($1,$2,$3,$4)", tenantID, person, token, outcome).Scan(&recorded)
	if recordErr != nil {
		return true, errors.Join(attemptErr, recordErr)
	}
	if !recorded {
		return true, errors.Join(attemptErr, v2input.ErrConflict)
	}
	return true, attemptErr
}
