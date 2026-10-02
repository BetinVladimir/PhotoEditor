package v2region

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// PrepareNode resolves scoped descendants, validates the complete node before
// uploading Params, then binds the returned immutable artifact. This is the
// preparation phase; the caller must still perform transactional job admission.
// An admission failure leaves a normal draft upload for existing expiry cleanup.
func (p FrozenRegionPlan) PrepareNode(ctx context.Context, who auth.Identity, task uuid.UUID, resolver RegionInputResolver, publisher RegionParamsPublisher, profile []byte) (FrozenRegionNode, error) {
	if err := ctx.Err(); err != nil {
		return FrozenRegionNode{}, err
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || publisher == nil || resolver == nil {
		return FrozenRegionNode{}, ErrRegionInvalid
	}
	// Freeze and validate runtime claims before calling external providers.
	placeholder := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("region.preview.node.preflight:1")), SHA256: p.digest}
	preflight, err := p.CompileNode(task, placeholder, p.InputRequirements(), profile)
	if err != nil {
		return FrozenRegionNode{}, err
	}
	var frozen regionNodeDocument
	if json.Unmarshal(preflight.JSON(), &frozen) != nil {
		return FrozenRegionNode{}, ErrRegionInvalid
	}
	resolved, err := p.ResolveInputs(ctx, resolver, who)
	if err != nil {
		return FrozenRegionNode{}, err
	}
	if _, err = p.CompileNode(task, placeholder, resolved, frozen.Profile); err != nil {
		return FrozenRegionNode{}, err
	}
	if err = ctx.Err(); err != nil {
		return FrozenRegionNode{}, err
	}
	params, err := p.PublishParams(ctx, who, task, publisher)
	if err != nil {
		return FrozenRegionNode{}, err
	}
	return p.CompileNode(task, params, resolved, frozen.Profile)
}
