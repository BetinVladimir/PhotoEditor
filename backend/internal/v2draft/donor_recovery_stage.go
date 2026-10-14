package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// RecoverAndStage reconstructs a qualified blocked donor task after restart.
// Intent read, object reload, Params publication and descriptor staging are
// separate bounded steps. Every SQL boundary rechecks authority; no transaction
// spans IO. A failure returns no usable frozen task; durable drafts/publications
// remain replayable. Input transport and final admission/queue are separate.
func (s SQLDonorParamsPublisher) RecoverAndStage(ctx context.Context, who auth.Identity, tenant, project, run, workflow uuid.UUID,
	loader DonorRecoveryLoader, manual DonorManualProducer) (FrozenDonorTask, error) {
	var zero FrozenDonorTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Publisher == nil || tenant == uuid.Nil || project == uuid.Nil || run == uuid.Nil || workflow == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	// Retain supplied original producer bytes across catalog/publisher callbacks.
	manual.RootRaw = appendOptionalBytes(manual.RootRaw)
	manual.ParamsRaw = appendOptionalBytes(manual.ParamsRaw)
	manual.VectorsRaw = appendOptionalBytes(manual.VectorsRaw)
	seed, err := s.ReadRecoverySeed(ctx, who, tenant, run, workflow)
	if err != nil {
		return zero, err
	}
	loaded, prepared, err := loader.Reload(ctx, who, tenant, project, workflow, seed, manual)
	if err != nil {
		return zero, err
	}
	published, err := s.Publish(ctx, who, tenant, run, workflow, seed.Ordinal(), loaded, prepared)
	if err != nil {
		return zero, err
	}
	frozen, err := FreezeQualifiedDonorTask(ctx, workflow, seed.Ordinal(), loaded, prepared, published)
	if err != nil {
		return zero, err
	}
	if err = (SQLDonorDescriptorStore{DB: s.DB}).Stage(ctx, who, tenant, run, frozen); err != nil {
		return zero, err
	}
	return frozen, nil
}
