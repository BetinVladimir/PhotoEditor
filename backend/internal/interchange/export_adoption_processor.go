package interchange

import (
	"context"
	"errors"
	"time"

	"photoeditor/backend/internal/v2input"
)

var ErrExportAdoptionLeaseLost = errors.New("export adoption lease lost")

type ExportAdoptionClaimer interface {
	NextExportAdoption(context.Context) (*ExportAdoptionClaim, error)
	RenewExportAdoptionClaim(context.Context, ExportAdoptionClaim) (bool, error)
}

type ExportAdoptionHandler interface {
	PrepareAndAdopt(context.Context, ExportAdoptionClaim) error
}

// SQLSchedulerExportAdoptionHandler installs the physically qualified closure
// and commits it under the same token. The SQL commit repeats every fence.
type SQLSchedulerExportAdoptionHandler struct {
	Reader SchedulerExportClosureReader
	Claims SQLExportAdoptionClaimStore
}

func (h SQLSchedulerExportAdoptionHandler) PrepareAndAdopt(ctx context.Context, claim ExportAdoptionClaim) error {
	if h.Claims.DB == nil || h.Reader.Archive.ResultReader.Claims.DB != h.Claims.DB ||
		h.Reader.Archive.ResultReader.Bucket == "" {
		return v2input.ErrInvalid
	}
	prepared, frozen, err := h.Reader.Prepare(ctx, claim)
	if err != nil {
		return err
	}
	return prepared.AdoptScheduler(ctx, h.Claims, claim, frozen,
		h.Reader.Archive.ResultReader.Bucket)
}

type SchedulerExportAdoptionProcessor struct {
	Claims  ExportAdoptionClaimer
	Handler ExportAdoptionHandler
	// HeartbeatInterval must stay well below the SQL claim's 31-minute lease.
	HeartbeatInterval time.Duration
}

// ProcessOne performs one bounded scheduler claim. A lost lease cancels all
// physical reads; success leaves terminal run finalization to the scheduler.
func (p SchedulerExportAdoptionProcessor) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p.Claims == nil || p.Handler == nil || p.HeartbeatInterval <= 0 ||
		p.HeartbeatInterval > 10*time.Minute {
		return false, v2input.ErrInvalid
	}
	claim, err := p.Claims.NextExportAdoption(ctx)
	if err != nil || claim == nil {
		return false, err
	}
	if err := claim.validate(); err != nil {
		return false, err
	}
	work, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(p.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-ticker.C:
				check, stop := context.WithTimeout(work, 30*time.Second)
				ok, renewErr := p.Claims.RenewExportAdoptionClaim(check, *claim)
				stop()
				if renewErr != nil {
					cancel(renewErr)
					return
				}
				if !ok {
					cancel(ErrExportAdoptionLeaseLost)
					return
				}
			}
		}
	}()
	err = p.Handler.PrepareAndAdopt(work, *claim)
	cause := context.Cause(work)
	cancel(nil)
	<-done
	if cause != nil {
		return false, cause
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
