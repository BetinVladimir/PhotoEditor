package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// Continues only the exact original public ordinary request. Person completion
// is not authority to bypass protection, qualification or parent admission.
type SQLOrdinaryParentRecovery struct {
	SQLNativePersonAdmissionRecovery
}

func (j SQLOrdinaryParentRecovery) Recover(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID) (bool, bool, error) {
	if err := j.validate(ctx); err != nil {
		return false, false, err
	}
	work, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()
	completed := false
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
		person, err := j.Preparer.Reader.restorePersonDraftPlan(work, catalog, who, anchor)
		if err != nil {
			return err
		}
		if person.prepared.continuation != "ordinary_parent" {
			return v2input.ErrConflict
		}
		policy := person.prepared.ordinaryPolicy
		if policy == nil || policy.ProjectID != j.Preparer.Reader.ProjectID {
			return v2input.ErrConflict
		}
		configuration, err := policy.preparation(anchor.Color)
		if err != nil {
			return err
		}
		if configuration.RAM > capacity.RAM || configuration.VRAM > capacity.VRAM || configuration.Scratch > capacity.Scratch || configuration.Output > capacity.Output || configuration.MetadataBytes > capacity.MetadataBytes || policy.Reserved > j.Preparer.ReservedBytes {
			return v2input.ErrInvalid
		}
		configuration, err = j.Preparer.Reader.selectOrdinaryTransferConfiguration(work, who, anchor.Request, anchor.Published, configuration)
		if err != nil {
			return err
		}
		preparer := j.Preparer
		preparer.ReservedBytes = policy.Reserved
		_, err = preparer.admitOrdinaryCreateRequest(work, who, anchor.Request, anchor.Key, configuration)
		completed = err == nil
		return err
	})
	if err != nil {
		return false, locked, err
	}
	return completed, locked, nil
}
func (j SQLOrdinaryParentRecovery) ProcessOne(ctx context.Context) (bool, error) {
	if err := j.validate(ctx); err != nil {
		return false, err
	}
	var tenantID, person, parent, token uuid.UUID
	var who auth.Identity
	leaseSeconds := int((j.Timeout+time.Second-1)/time.Second) + 10
	claim, stop := context.WithTimeout(ctx, 5*time.Second)
	err := j.Preparer.Scheduler.QueryRow(claim, "SELECT tenant_id,person_run_id,parent_run_id,actor_id,session_id,auth_epoch,lease_token FROM photo.claim_v27_ordinary_parent($1,$2)", j.Preparer.Reader.ProjectID, leaseSeconds).Scan(&tenantID, &person, &parent, &who.UserID, &who.SessionID, &who.Epoch, &token)
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
			outcome = "completed"
		}
	}
	finish, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var recorded bool
	recordErr := j.Preparer.Scheduler.QueryRow(finish, "SELECT photo.record_v27_ordinary_parent($1,$2,$3,$4)", tenantID, person, token, outcome).Scan(&recorded)
	if recordErr != nil {
		return true, errors.Join(attemptErr, recordErr)
	}
	if !recorded {
		return true, errors.Join(attemptErr, v2input.ErrConflict)
	}
	return true, attemptErr
}
