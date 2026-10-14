package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"sync/atomic"
)

// Documents must provide the dedicated SQL-backed transport reservation and
// completion checks. This adapter validates bytes/scope before invoking it.
type PerspectiveLinesTransportPublisher struct {
	plan            PerspectiveLinesPlan
	params          review.ArtifactRef
	documents       v2input.DocumentPublisher
	chunk, manifest *v2input.Ref
	busy            atomic.Bool
}

func NewPerspectiveLinesTransportPublisher(plan PerspectiveLinesPlan, params review.ArtifactRef, documents v2input.DocumentPublisher) (*PerspectiveLinesTransportPublisher, error) {
	if documents == nil {
		return nil, v2input.ErrInvalid
	}
	if _, err := PreparePerspectiveLinesTask(plan, params); err != nil {
		return nil, err
	}
	owned, err := CompilePerspectiveLinesPlan(plan.scope, plan.image, plan.workingICC)
	if err != nil {
		return nil, err
	}
	return &PerspectiveLinesTransportPublisher{plan: owned, params: params, documents: documents}, nil
}
func (p *PerspectiveLinesTransportPublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if p == nil || task != p.plan.task || !p.busy.CompareAndSwap(false, true) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	defer p.busy.Store(false)
	var previous *v2input.Ref
	if contract == "InputClosureManifest" {
		previous = p.chunk
	}
	doc, err := CompilePerspectiveLinesTransportDocument(p.plan, p.params, ordinal, contract, body, previous)
	if err != nil {
		return v2input.Ref{}, err
	}
	result, err := p.documents.Publish(ctx, doc.Scope(), doc.Bytes())
	if err != nil {
		return v2input.Ref{}, err
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	ref := v2input.Ref{ArtifactID: result.ID, SHA256: result.SHA256}
	if ref.ArtifactID == uuid.Nil || ref.SHA256 != doc.SHA256() {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	entries, err := p.plan.Entries(p.params)
	if err != nil {
		return v2input.Ref{}, err
	}
	for _, entry := range entries {
		if entry.Ref.ArtifactID == ref.ArtifactID {
			return v2input.Ref{}, v2input.ErrInvalid
		}
	}
	if contract == "InputClosureChunk" {
		if (p.chunk != nil && *p.chunk != ref) || (p.manifest != nil && p.manifest.ArtifactID == ref.ArtifactID) {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		p.chunk = &ref
	} else {
		if p.chunk == nil || p.chunk.ArtifactID == ref.ArtifactID || (p.manifest != nil && *p.manifest != ref) {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		p.manifest = &ref
	}
	return ref, nil
}
