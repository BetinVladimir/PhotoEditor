package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type DeliveryExpansionStore interface {
	Claim(context.Context) (*delivery.ExpansionLease, error)
	LoadPlan(context.Context, delivery.ExpansionLease) (delivery.PreparePlan, error)
	Renew(context.Context, delivery.ExpansionLease) error
	Release(context.Context, delivery.ExpansionLease) error
}

type DeliveryChildProcessor interface {
	ProcessChild(context.Context, delivery.ExpansionLease, delivery.PreparePlan, delivery.ExportChildIntent) (DeliveryChildAdmission, error)
}

// DeliveryChildObserver advances admitted children without advancing the parent admission cursor.
type DeliveryChildObserver interface {
	PendingSlots(context.Context, delivery.ExpansionLease, int) ([]int, error)
	ObserveChild(context.Context, delivery.ExpansionLease, delivery.PreparePlan, delivery.ExportChildIntent) (bool, error)
}

type DeliveryAssemblyProcessor interface {
	ProcessAssembly(context.Context, delivery.ExpansionLease, delivery.PreparePlan) (string, error)
}

// DeliverySigningProcessor owns the required-provenance child stage. "adopted"
// means every required signed result has passed its separate adoption gate;
// scanning means another bounded page should run promptly; queued signer jobs
// return pending after a full sweep and cannot advance assembly.
type DeliverySigningProcessor interface {
	ProcessSigning(context.Context, delivery.ExpansionLease, delivery.PreparePlan) (string, error)
}

type DeliveryExpansionProgress struct {
	AssemblyState string
	SigningState  string
	Observed      int
	Claimed       bool
	Admitted      int
	Cursor        int
	SlotCount     int
}

// DeliveryExpansionDriver handles a bounded page under one SQL lease. It never
// treats admitted exports as completed exports; the assembly processor must prove
// the complete terminal matrix before authorizing activation.
// Release is best effort on failure; a lost lease expires without cursor credit.
// Work exceeding one renewal interval is fenced by publication/admission SQL and
// must retry after reacquiring authority; there is no concurrent renewal goroutine.
type DeliveryExpansionDriver struct {
	Store    DeliveryExpansionStore
	Children DeliveryChildProcessor
	Observer DeliveryChildObserver
	Assembly DeliveryAssemblyProcessor
	SignedAssembly DeliveryAssemblyProcessor
	Signing  DeliverySigningProcessor
	PageSize int
}

func (d DeliveryExpansionDriver) ProcessOne(ctx context.Context) (progress DeliveryExpansionProgress, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if d.Store == nil || d.Children == nil || d.PageSize < 1 || d.PageSize > 100 {
		return progress, v2input.ErrInvalid
	}
	lease, err := d.Store.Claim(ctx)
	if err != nil || lease == nil {
		return progress, err
	}
	progress = DeliveryExpansionProgress{Claimed: true, Cursor: lease.Cursor, SlotCount: lease.SlotCount}
	defer func() {
		// Do not suppress a processing error with cleanup failure. Cancellation must
		// not trigger an unbounded background SQL call; expiry remains recovery.
		if ctx.Err() == nil && progress.AssemblyState != "activated" && progress.AssemblyState != "finalized_failed" {
			releaseErr := d.Store.Release(ctx, *lease)
			if err == nil {
				err = releaseErr
			}
		}
	}()
	plan, err := d.Store.LoadPlan(ctx, *lease)
	if err != nil {
		return progress, err
	}
	if d.Observer != nil {
		slots, observeErr := d.Observer.PendingSlots(ctx, *lease, d.PageSize)
		if observeErr != nil {
			return progress, observeErr
		}
		if len(slots) > d.PageSize {
			return progress, v2input.ErrInvalid
		}
		previous := -1
		for _, slot := range slots {
			if slot <= previous || slot < 0 || slot >= lease.Cursor {
				return progress, v2input.ErrInvalid
			}
			previous = slot
		}
		for _, slot := range slots {
			if err = ctx.Err(); err != nil {
				return progress, err
			}
			if err = d.Store.Renew(ctx, *lease); err != nil {
				return progress, err
			}
			childPage, childErr := plan.ExportChildren(lease.Run, slot, 1)
			if childErr != nil {
				return progress, childErr
			}
			if len(childPage.Items) != 1 || childPage.Items[0].SlotIndex != slot {
				return progress, v2input.ErrInvalid
			}
			changed, childErr := d.Observer.ObserveChild(ctx, *lease, plan, childPage.Items[0])
			if childErr != nil {
				return progress, childErr
			}
			if changed {
				progress.Observed++
			}
		}
	}
	page, err := plan.ExportChildren(lease.Run, lease.Cursor, d.PageSize)
	if err != nil {
		return progress, err
	}
	for _, child := range page.Items {
		if err = ctx.Err(); err != nil {
			return progress, err
		}
		if err = d.Store.Renew(ctx, *lease); err != nil {
			return progress, err
		}
		result, processErr := d.Children.ProcessChild(ctx, *lease, plan, child)
		if processErr != nil {
			return progress, processErr
		}
		if result.RunID != child.RunID || result.ExportID == uuid.Nil || result.NextCursor != child.SlotIndex+1 {
			return progress, v2input.ErrInvalid
		}
		lease.Cursor = result.NextCursor
		progress.Cursor = lease.Cursor
		progress.Admitted++
	}
	if lease.Cursor == lease.SlotCount {
		if d.Assembly == nil { return progress, v2input.ErrInvalid }
		mode, modeErr := plan.ProvenanceMode()
		if modeErr != nil { return progress, modeErr }
		assembly := d.Assembly
		if mode == "required" {
			if d.Signing == nil || d.SignedAssembly == nil { return progress, v2input.ErrInvalid }
			assembly = d.SignedAssembly
			if err = d.Store.Renew(ctx, *lease); err != nil {
				return progress, err
			}
			state, signingErr := d.Signing.ProcessSigning(ctx, *lease, plan)
			if signingErr != nil { return progress, signingErr }
			if state != "scanning" && state != "pending" && state != "adopted" && state != "finalized_failed" {
				return progress, v2input.ErrInvalid
			}
			progress.SigningState = state
			if state == "finalized_failed" {
				progress.AssemblyState = "finalized_failed"
				return progress, nil
			}
			if state != "adopted" { return progress, nil }
		}
		if err = d.Store.Renew(ctx, *lease); err != nil {
			return progress, err
		}
		state, assemblyErr := assembly.ProcessAssembly(ctx, *lease, plan)
		if assemblyErr != nil {
			return progress, assemblyErr
		}
		if state != "pending" && state != "finalized_failed" && state != "activated" {
			return progress, v2input.ErrInvalid
		}
		progress.AssemblyState = state
	}
	return progress, nil
}
