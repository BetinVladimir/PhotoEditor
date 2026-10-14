package semanticproposal

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Project-scoped durable continuation. SQL discovery supplies original actor and
// session; all admission and physical provenance checks remain in the preparer.
// A session lock spans object IO, while persisted leases fence retry recording.
type SQLNativeManualParentRecovery struct {
	Preparer               TransferDraftPreparer
	Catalog                v2draft.ArtifactCatalog
	CatalogForReference    NativeParentArtifactCatalogFactory
	DiscoverPersonPolicy   bool
	DiscoverTransferPolicy bool
	Timeout                time.Duration
}

func (j SQLNativeManualParentRecovery) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if j.Preparer.Scheduler == nil || j.Preparer.Scheduler.Config().MaxConns < 2 || (j.Catalog == nil && j.CatalogForReference == nil) || (j.Preparer.Configuration.PersonProtection == nil && !j.DiscoverPersonPolicy) || j.Preparer.Reader.DB == nil || j.Preparer.Reader.Objects == nil || j.Preparer.Reader.Bucket == "" || j.Preparer.Reader.ProjectID == uuid.Nil || j.Timeout <= 0 || j.Timeout > 29*time.Minute {
		return v2input.ErrInvalid
	}
	if j.DiscoverTransferPolicy {
		return validateTransferCapacityConfiguration(j.Preparer.Configuration)
	}
	return validateTransferPolicyConfiguration(ownTransferPreparation(j.Preparer.Configuration))
}

// One bounded attempt. No SQL transaction is held during model/object IO.
func (j SQLNativeManualParentRecovery) Recover(ctx context.Context, who auth.Identity, tenantID, parent uuid.UUID) (nativeManualParentAdvance, bool, error) {
	var out nativeManualParentAdvance
	if err := j.validate(ctx); err != nil {
		return out, false, err
	}
	work, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()
	done, err := withNativeManualParentLock(work, j.Preparer.Scheduler, tenantID, parent, func(work context.Context) error {
		configuration := ownTransferPreparation(j.Preparer.Configuration)
		anchor, err := j.Preparer.Reader.loadNativePreparationAnchor(work, who, tenantID, parent, configuration.Color, configuration.MetadataBytes)
		if err != nil {
			return err
		}
		if j.DiscoverTransferPolicy {
			configuration, err = j.Preparer.Reader.prepareNativeTransferRuntimeConfiguration(work, who, tenantID, configuration)
			if err != nil {
				return err
			}
		}
		if configuration.PersonProtection == nil && j.DiscoverPersonPolicy {
			person, err := j.Preparer.Reader.selectNativePersonProtectionPolicy(work, who, tenantID, configuration.MetadataBytes)
			if err != nil {
				return err
			}
			configuration.PersonProtection = &person
		}
		catalog, err := resolveNativeParentRecoveryCatalog(work, who, anchor, j.Preparer.Reader.ProjectID, j.Catalog, j.CatalogForReference)
		if err != nil {
			return err
		}
		preparer := j.Preparer
		preparer.Configuration = configuration
		out, err = preparer.advanceNativeManualParent(work, catalog, who, anchor)
		return err
	})
	if err != nil || !done {
		return nativeManualParentAdvance{}, done, err
	}
	return out, true, nil
}

func nativeManualParentRecoveryOutcome(parent uuid.UUID, out nativeManualParentAdvance, attemptErr error) (string, error) {
	if attemptErr != nil {
		return "retry", attemptErr
	}
	switch out.phase {
	case nativePreparationBlocked, nativePreparationWaiting:
		if out.ticket != (v2input.JobTicket{}) {
			return "retry", v2input.ErrConflict
		}
		return "waiting", nil
	case nativePreparationFailed:
		if out.ticket != (v2input.JobTicket{}) {
			return "retry", v2input.ErrConflict
		}
		return "failed", nil
	case nativePreparationSucceeded:
		workflow, err := v2input.WorkflowID(parent)
		if err != nil || out.ticket.RunID != parent || out.ticket.WorkflowID != workflow || out.ticket.State != "queued" {
			return "retry", v2input.ErrConflict
		}
		return "completed", nil
	default:
		return "retry", v2input.ErrConflict
	}
}

// Persisted due times and SKIP LOCKED claims survive process restarts. Unknown
// claim outcomes expire automatically; stale tokens cannot record completion.
func (j SQLNativeManualParentRecovery) ProcessOne(ctx context.Context) (bool, error) {
	if err := j.validate(ctx); err != nil {
		return false, err
	}
	var tenantID, detector, parent, token uuid.UUID
	var who auth.Identity
	leaseSeconds := int((j.Timeout+time.Second-1)/time.Second) + 10
	claim, stop := context.WithTimeout(ctx, 5*time.Second)
	err := j.Preparer.Scheduler.QueryRow(claim, "SELECT tenant_id,detector_run_id,parent_run_id,actor_id,session_id,auth_epoch,lease_token FROM photo.claim_v27_native_manual_parent($1,$2)", j.Preparer.Reader.ProjectID, leaseSeconds).Scan(&tenantID, &detector, &parent, &who.UserID, &who.SessionID, &who.Epoch, &token)
	stop()
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if tenantID == uuid.Nil || detector == uuid.Nil || parent == uuid.Nil || token == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return true, v2input.ErrConflict
	}
	out, locked, attemptErr := j.Recover(ctx, who, tenantID, parent)
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	outcome := "waiting"
	if locked || attemptErr != nil {
		outcome, attemptErr = nativeManualParentRecoveryOutcome(parent, out, attemptErr)
	}
	finish, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var recorded bool
	recordErr := j.Preparer.Scheduler.QueryRow(finish, "SELECT photo.record_v27_native_manual_parent($1,$2,$3,$4)", tenantID, detector, token, outcome).Scan(&recorded)
	if recordErr != nil {
		return true, errors.Join(attemptErr, recordErr)
	}
	if !recorded {
		return true, errors.Join(attemptErr, v2input.ErrConflict)
	}
	return true, attemptErr
}
