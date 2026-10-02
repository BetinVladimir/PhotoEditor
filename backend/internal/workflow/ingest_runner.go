package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

type IngestContextSource interface {
	Load(context.Context, uuid.UUID, uuid.UUID) (IngestRunContext, error)
}
type IngestInspectionBuilder interface {
	PrepareInspection(context.Context, uuid.UUID, IngestSpec) error
}
type IngestDecodeAdvance interface {
	Advance(context.Context, uuid.UUID, IngestSpec) (bool, error)
}

type IngestRunner struct {
	Runs       IngestContextSource
	Inspection IngestInspectionBuilder
	Decode     IngestDecodeAdvance
	Catalog    IngestDecodeAdvance
}

// Advance always reloads durable phase. A completed seal whose response was lost
// is therefore reconciled by the next invocation without replaying publication.
// Callers serialize per-run work; SQL mutators remain the authority on races.
// false means no confirmed transition; partial file commits may still exist.
func (r IngestRunner) Advance(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	if r.Runs == nil || r.Inspection == nil || r.Decode == nil || tenant == uuid.Nil || run == uuid.Nil {
		return false, errors.New("invalid ingest runner")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	current, err := r.Runs.Load(ctx, tenant, run)
	if err != nil {
		return false, err
	}
	if current.Spec.RunID != run {
		return false, errors.New("ingest context run mismatch")
	}
	switch current.Phase {
	case "planning":
		if err = r.Inspection.PrepareInspection(ctx, tenant, current.Spec); err != nil {
			return false, err
		}
		return true, nil
	case "inspection", "decode_build":
		return r.Decode.Advance(ctx, tenant, current.Spec)
	case "decode", "catalog":
		if r.Catalog == nil {
			return false, errors.New("ingest catalog coordinator required")
		}
		return r.Catalog.Advance(ctx, tenant, current.Spec)
	default:
		return false, errors.New("unsupported ingest phase")
	}
}
