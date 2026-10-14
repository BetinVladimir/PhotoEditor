package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

var _ DeliveryChildObserver = DeliveryChildPipeline{}

func (p DeliveryChildPipeline) PendingSlots(ctx context.Context, lease delivery.ExpansionLease, limit int) ([]int, error) {
	if p.DB == nil {
		return nil, v2input.ErrInvalid
	}
	return readOrdinaryDeliveryPendingSlots(ctx, p.DB, lease, limit)
}

// ObserveChild reconstructs a maximum two-stage prefix from exact physical
// producer outputs. SQL repeats authority/fence checks at every mutation. No
// object IO occurs in transactions. Encode completion remains a separate path.
func (p DeliveryChildPipeline) ObserveChild(ctx context.Context, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p.DB == nil || p.Objects == nil || p.Reader == nil || p.Resolver == nil {
		return false, v2input.ErrInvalid
	}
	if _, _, err := plan.ExportRequest(lease.Run, child); err != nil {
		return false, err
	}
	source, err := p.Resolver.ResolveChild(ctx, lease, plan, child)
	if err != nil {
		return false, err
	}
	source.ImageJCS = append([]byte(nil), source.ImageJCS...)
	entries := append([]v2input.Entry(nil), source.Entries...)
	prefix := make([]DeliveryPresentationOutput, 0, 2)
	for len(prefix) < 2 {
		pending, err := CompileDeliveryContinuation(plan, lease.Run, child, source.ImageJCS, prefix, source.TargetICCSHA, p.Registry, p.Limits)
		if err != nil {
			return false, err
		}
		if pending.Node.Operation == "export.encode" {
			return false, nil
		}
		verified, err := LoadVerifiedDeliveryStage(ctx, p.DB, p.Reader, p.Bucket, lease, plan, child, source.ImageJCS, prefix, source.TargetICCSHA, p.Registry, p.Limits)
		if err != nil || verified == nil {
			return false, err
		}
		prefix = append(prefix, verified.Output())
		entries, err = deliveryStageEntries(entries, verified.Producer())
		if err != nil {
			return false, err
		}
		next, err := CompileDeliveryContinuation(plan, lease.Run, child, source.ImageJCS, prefix, source.TargetICCSHA, p.Registry, p.Limits)
		if err != nil {
			return false, err
		}
		saved, err := ReadSavedDeliveryContinuationParams(ctx, p.DB, lease, child, next.Node)
		if err != nil {
			return false, err
		}
		freeze := func(ref v2input.Ref) (FrozenDeliveryContinuationTask, error) {
			return FreezeDeliveryContinuationTask(plan, lease.Run, child, source.ImageJCS, prefix, source.TargetICCSHA, p.Registry, p.Limits, ref, entries)
		}
		actions := deliveryContinuationActions{
			activated: func(ref v2input.Ref) (bool, error) {
				frozen, err := freeze(ref)
				if err != nil {
					return false, err
				}
				return (sqlContinuationAdmission{db: p.DB, frozen: frozen, lease: lease, plan: plan, child: child}).Activated(ctx)
			},
			open: func() error {
				return database.InTransaction(ctx, p.DB, func(tx pgx.Tx) error {
					if len(prefix) == 1 {
						return verified.BeginContinuationInTransaction(ctx, tx, lease, plan, child, pending.Node.Operation, p.Bucket)
					}
					return verified.ReopenContinuationInTransaction(ctx, tx, lease, plan, child, p.Bucket)
				})
			},
			publish: func() (v2input.Ref, error) {
				return PublishDeliveryContinuationParams(ctx, p.DB, p.Objects, p.Bucket, lease, plan, child, source.ImageJCS, prefix, source.TargetICCSHA, p.Registry, p.Limits)
			},
			admit: func(ref v2input.Ref) (bool, error) {
				frozen, err := freeze(ref)
				if err != nil {
					return false, err
				}
				documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: p.DB, Bucket: p.Bucket}, Objects: p.Objects}
				publisher := v2input.ScopedPublisher{Documents: documents, Tenant: lease.Tenant, Run: child.RunID}
				return frozen.AdmitContinuation(ctx, p.DB, publisher, lease, plan, child)
			},
		}
		recovered, err := resumeDeliveryContinuation(ctx, saved, actions)
		if err != nil {
			return false, err
		}
		if recovered && next.Node.Operation == "derivative.sharpen" {
			continue
		}
		return !recovered, nil
	}
	return false, nil
}

// Only physically verified producer outputs may extend the authorized inventory.
// Conflicting aliases are rejected before freezing/publishing the next task.
func deliveryStageEntries(entries []v2input.Entry, producer DeliveryStageResult) ([]v2input.Entry, error) {
	if len(producer.Outputs) < 3 || len(producer.Outputs) > 4 {
		return nil, v2input.ErrInvalid
	}
	result := append([]v2input.Entry(nil), entries...)
	roles := make(map[string]bool, len(producer.Outputs))
	ids := make(map[uuid.UUID]bool, len(producer.Outputs))
	for _, output := range producer.Outputs {
		if roles[output.Role] || output.Ref.ArtifactID == uuid.Nil || ids[output.Ref.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		roles[output.Role] = true
		ids[output.Ref.ArtifactID] = true
		contract := ""
		switch output.Role {
		case "working_image":
			contract = "WorkingImage"
		case "derivative_report":
			contract = "DerivativeReport"
		case "working_rgb", "working_alpha":
			contract = "NpyArray"
		default:
			return nil, v2input.ErrInvalid
		}
		found := false
		for _, entry := range result {
			if entry.Ref.ArtifactID == output.Ref.ArtifactID {
				if entry.Ref != output.Ref || entry.Contract != contract {
					return nil, v2input.ErrInvalid
				}
				found = true
			}
		}
		if !found {
			result = append(result, v2input.Entry{Ref: output.Ref, Contract: contract})
		}
	}
	if !roles["working_image"] || !roles["derivative_report"] || !roles["working_rgb"] {
		return nil, v2input.ErrInvalid
	}
	return result, nil
}

// These actions bind one immutable prefix/next descriptor. Recovery precedes
// barrier mutation because the previously activated graph can already be sealed.
// Errors stop before later side effects; retries reuse ready Params.
type deliveryContinuationActions struct {
	activated func(v2input.Ref) (bool, error)
	open      func() error
	publish   func() (v2input.Ref, error)
	admit     func(v2input.Ref) (bool, error)
}

func resumeDeliveryContinuation(ctx context.Context, saved *v2input.Ref, a deliveryContinuationActions) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if a.activated == nil || a.open == nil || a.publish == nil || a.admit == nil {
		return false, v2input.ErrInvalid
	}
	if saved != nil {
		recovered, err := a.activated(*saved)
		if err != nil || recovered {
			return recovered, err
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := a.open(); err != nil {
		return false, err
	}
	var ref v2input.Ref
	if saved != nil {
		ref = *saved
	} else {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		var err error
		ref, err = a.publish()
		if err != nil {
			return false, err
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return a.admit(ref)
}
