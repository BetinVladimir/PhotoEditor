package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type RenderBatchStore interface {
	BeginBatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, auth.Identity, string, []CompiledRenderBatchPart) error
	LoadBatch(context.Context, uuid.UUID, uuid.UUID, auth.Identity) (RecoveredRenderBatch, error)
	LoadBatchPart(context.Context, uuid.UUID, uuid.UUID, auth.Identity, int) (RecoveredRenderDraft, error)
}

type RenderBatchCoordinator struct {
	Store  RenderBatchStore
	Inputs RenderDraftCoordinator
}

// RecoverInputs validates ALL persisted children and freshly resolved input
// digests before publishing any part. It preserves frozen execution choices.
// Current SQL draft/transport scope is still required at publication time.
func (c RenderBatchCoordinator) RecoverInputs(ctx context.Context, tenant, batch uuid.UUID, who auth.Identity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Store == nil || c.Inputs.Inputs == nil || c.Inputs.Transport == nil || c.Inputs.Resolver == nil || tenant == uuid.Nil || batch == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	parent, err := c.Store.LoadBatch(ctx, tenant, batch, who)
	if err != nil {
		return err
	}
	parent.Manifest.Parts = append([]RenderBatchPartEvidence(nil), parent.Manifest.Parts...)
	if parent.BatchID != batch || parent.CancelRequested || parent.Manifest.SessionID != who.SessionID {
		return v2input.ErrExpired
	}
	if err := validateRenderBatchManifest(batch, parent.Manifest); err != nil {
		return err
	}
	sha, err := canonical.Hash(parent.Manifest)
	if err != nil || sha != parent.ManifestSHA {
		return v2input.ErrInvalid
	}
	type prepared struct {
		saved  RecoveredRenderDraft
		nodes  []RecoveredRenderNode
		drafts []v2input.DraftTask
	}
	preparedParts := make([]prepared, len(parent.Manifest.Parts))
	for i, part := range parent.Manifest.Parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		saved, err := c.Store.LoadBatchPart(ctx, tenant, batch, who, i)
		if err != nil {
			return err
		}
		saved.Config = append(json.RawMessage(nil), saved.Config...)
		if saved.RunID != part.RunID || saved.ProjectID != parent.Manifest.ProjectID || saved.SnapshotID != parent.Manifest.SnapshotID || saved.ConfigSHA != part.ConfigSHA || saved.PolicySHA != parent.Manifest.PolicySHA || saved.ReservedBytes != part.OutputBytes {
			return v2input.ErrInvalid
		}
		nodes, drafts, err := c.Inputs.prepareRecoveredInputs(ctx, tenant, who, saved)
		if err != nil {
			return err
		}
		if len(nodes) != part.FrameCount {
			return v2input.ErrInvalid
		}
		preparedParts[i] = prepared{saved, nodes, drafts}
	}
	for _, part := range preparedParts {
		if err := c.Inputs.publishRecoveredInputs(ctx, tenant, who, part.saved, part.nodes, part.drafts); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Prepare resolves and compiles the entire frozen batch, atomically stages it,
// then uses the same persisted-evidence recovery path for input publication.
// Returned plans are retained on uncertain commit/publication failure for retry.
func (c RenderBatchCoordinator) Prepare(ctx context.Context, batch, project, snapshot uuid.UUID, policySHA string, requests []RenderDraftRequest) ([]CompiledRenderBatchPart, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Store == nil || c.Inputs.Inputs == nil || c.Inputs.Transport == nil || c.Inputs.Resolver == nil || batch == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || len(requests) < 1 || len(requests) > 5000 || len(policySHA) != 64 {
		return nil, v2input.ErrInvalid
	}
	for _, ch := range policySHA {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return nil, v2input.ErrInvalid
		}
	}
	owner := requests[0]
	if owner.Tenant == uuid.Nil || owner.Actor.UserID == uuid.Nil || owner.Actor.SessionID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	for _, r := range requests {
		if r.Tenant != owner.Tenant || r.Actor != owner.Actor {
			return nil, v2input.ErrInvalid
		}
	}
	owned := append([]RenderDraftRequest(nil), requests...)
	for i := range owned {
		owned[i].RecipeRaw = append([]byte(nil), owned[i].RecipeRaw...)
		owned[i].SourceRaw = append([]byte(nil), owned[i].SourceRaw...)
		owned[i].LayerRaw = append([]byte(nil), owned[i].LayerRaw...)
		owned[i].ProtectionRaw = append([]byte(nil), owned[i].ProtectionRaw...)
		owned[i].LayerDocuments = append([]RenderLayerDocument(nil), owned[i].LayerDocuments...)
	}
	inventories := make([][]v2input.Entry, len(owned))
	for i, r := range owned {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		evidence, err := resolveRenderClosureEvidence(ctx, r, c.Inputs.Resolver)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		owned[i].ClosureDocuments = evidence.Documents
		owned[i].ClosureOccurrences = evidence.Occurrences
		inventories[i] = evidence.Entries
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	plans, err := CompileRenderBatchPlans(batch, project, snapshot, owned, inventories)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.Store.BeginBatch(ctx, owner.Tenant, batch, project, snapshot, owner.Actor, policySHA, plans); err != nil {
		return plans, err
	}
	return plans, c.RecoverInputs(ctx, owner.Tenant, batch, owner.Actor)
}
