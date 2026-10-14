package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type RenderDraftStore interface {
	Begin(context.Context, uuid.UUID, uuid.UUID, auth.Identity, string, CompiledRenderRun) error
}
type RenderInputRegistrar interface {
	RegisterRenderInputs(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int, v2input.DraftTask, v2input.GraphTask) error
}
type RenderClosurePublisher interface {
	Prepare(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, v2input.SessionVersionBinding, v2input.Ref, []byte, []byte, CompositionInputResolver) (v2input.GraphTask, error)
}

// RenderDraftCoordinator connects scoped compilation to durable draft/input
// registration. Each store transaction completes before external publication.
// Repeating the same run is recoverable through immutable store/publication
// replay; successful registration does not qualify providers or activate tasks.
type RenderDraftCoordinator struct {
	Store     RenderDraftStore
	Inputs    RenderInputRegistrar
	Transport RenderClosurePublisher
	Resolver  CompositionInputResolver
}

func (c RenderDraftCoordinator) Prepare(ctx context.Context, run, project, snapshot uuid.UUID, policySHA string, requests []RenderDraftRequest) (CompiledRenderRun, error) {
	if err := ctx.Err(); err != nil {
		return CompiledRenderRun{}, err
	}
	if c.Store == nil || c.Inputs == nil || c.Transport == nil || c.Resolver == nil || len(requests) < 1 || len(requests) > 5000 || run == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || len(policySHA) != 64 {
		return CompiledRenderRun{}, v2input.ErrInvalid
	}
	for _, c := range policySHA {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return CompiledRenderRun{}, v2input.ErrInvalid
		}
	}
	// Own document evidence before any injected dependency receives control.
	owned := append([]RenderDraftRequest(nil), requests...)
	for i := range owned {
		owned[i].RecipeRaw = append([]byte(nil), owned[i].RecipeRaw...)
		owned[i].SourceRaw = append([]byte(nil), owned[i].SourceRaw...)
		owned[i].LayerRaw = append([]byte(nil), owned[i].LayerRaw...)
		owned[i].ProtectionRaw = append([]byte(nil), owned[i].ProtectionRaw...)
		owned[i].LayerDocuments = append([]RenderLayerDocument(nil), owned[i].LayerDocuments...)
	}
	resolved := make([][]v2input.Entry, len(owned))
	evidenceBytes := 0
	for i, r := range owned {
		if err := ctx.Err(); err != nil {
			return CompiledRenderRun{}, err
		}
		evidence, err := resolveRenderClosureEvidence(ctx, r, c.Resolver)
		if err != nil {
			return CompiledRenderRun{}, err
		}
		if err := ctx.Err(); err != nil {
			return CompiledRenderRun{}, err
		}
		// These payloads alone are a lower bound on the inline config size.
		// Stop before resolving the rest of a selection that cannot persist.
		for _, document := range evidence.Documents {
			if err := ctx.Err(); err != nil {
				return CompiledRenderRun{}, err
			}
			if len(document.JCS) > 1048576-evidenceBytes {
				return CompiledRenderRun{}, v2input.ErrInvalid
			}
			evidenceBytes += len(document.JCS)
		}
		for _, occurrence := range evidence.Occurrences {
			if err := ctx.Err(); err != nil {
				return CompiledRenderRun{}, err
			}
			encoded, encodeErr := canonical.Marshal(occurrence)
			if encodeErr != nil {
				return CompiledRenderRun{}, encodeErr
			}
			if len(encoded) > 1048576-evidenceBytes {
				return CompiledRenderRun{}, v2input.ErrInvalid
			}
			evidenceBytes += len(encoded)
		}
		owned[i].ClosureDocuments = evidence.Documents
		owned[i].ClosureOccurrences = evidence.Occurrences
		resolved[i] = evidence.Entries
	}
	if err := ctx.Err(); err != nil {
		return CompiledRenderRun{}, err
	}
	plan, err := compileRenderRunPlanForQueue(run, project, snapshot, owned, resolved,"interactive")
	if err != nil {
		return CompiledRenderRun{}, err
	}
	if err := ctx.Err(); err != nil {
		return CompiledRenderRun{}, err
	}
	if err = c.Store.Begin(ctx, owned[0].Tenant, run, owned[0].Actor, policySHA, plan); err != nil {
		// Commit may have succeeded before connection loss. Retain exact
		// evidence for an idempotent retry; this does not assert persistence.
		return plan, err
	}
	for i, r := range owned {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		graph, err := c.Transport.Prepare(ctx, r.Actor, r.Tenant, run, plan.Tasks[i].ID, r.Binding, r.RecipeRef, r.RecipeRaw, r.SourceRaw, c.Resolver)
		if err != nil {
			return plan, err
		}
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		if err = c.Inputs.RegisterRenderInputs(ctx, r.Tenant, run, plan.Workflow, i, plan.Tasks[i], graph); err != nil {
			return plan, err
		}
	}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	return plan, nil
}
