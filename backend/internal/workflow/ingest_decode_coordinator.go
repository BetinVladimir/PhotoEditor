package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
)

func (r IngestPlanRepository) SealDecode(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return false, errors.New("invalid decode barrier")
	}
	var sealed bool
	err := r.DB.QueryRow(ctx, "SELECT photo.seal_ingest_decode($1,$2)", tenant, run).Scan(&sealed)
	return sealed, err
}

type IngestDecodeWriter interface {
	BeginDecode(context.Context, uuid.UUID, uuid.UUID, IngestDecodePlan) (bool, error)
	Materialize(context.Context, uuid.UUID, uuid.UUID, PreparedTask, *uuid.UUID) (bool, error)
	SealDecode(context.Context, uuid.UUID, uuid.UUID) (bool, error)
}
type IngestDecodeCoordinator struct{ Tasks IngestDecodeWriter }

// Build consumes a fully checked plan. Resume during decode_build is safe; phase
// discovery must avoid rebuilding a graph already sealed for active decoding.
func (c IngestDecodeCoordinator) Build(ctx context.Context, tenant, run uuid.UUID, plan IngestDecodePlan) (bool, error) {
	if c.Tasks == nil || tenant == uuid.Nil || run == uuid.Nil {
		return false, errors.New("invalid decode coordinator")
	}
	if !plan.Ready {
		return false, nil
	}
	begun, err := c.Tasks.BeginDecode(ctx, tenant, run, plan)
	if err != nil {
		return false, err
	}
	if !begun {
		return false, errors.New("stale decode phase plan")
	}
	for _, node := range plan.Tasks {
		if err = ctx.Err(); err != nil {
			return false, err
		}
		metadata := node.Metadata.ID
		ok, err := c.Tasks.Materialize(ctx, tenant, run, node.Task, &metadata)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, errors.New("decode task binding conflict")
		}
	}
	sealed, err := c.Tasks.SealDecode(ctx, tenant, run)
	if err != nil {
		return false, err
	}
	if !sealed {
		return false, errors.New("decode barrier incomplete")
	}
	return true, nil
}
