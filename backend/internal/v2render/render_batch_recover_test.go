package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type renderBatchRecoveryProbe struct {
	begins      int
	failBegin   bool
	cancelBegin context.CancelFunc
	parent      RecoveredRenderBatch
	parts       []RecoveredRenderDraft
	failPart    int
	loads       int
}

func (p *renderBatchRecoveryProbe) LoadBatch(context.Context, uuid.UUID, uuid.UUID, auth.Identity) (RecoveredRenderBatch, error) {
	return p.parent, nil
}
func (p *renderBatchRecoveryProbe) LoadBatchPart(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ auth.Identity, index int) (RecoveredRenderDraft, error) {
	p.loads++
	if index == p.failPart {
		return RecoveredRenderDraft{}, v2input.ErrExpired
	}
	return p.parts[index], nil
}

type renderBatchLateDrift struct {
	entries []v2input.Entry
	calls   int
}

func (p *renderBatchLateDrift) ResolveTaskInputs(context.Context, auth.Identity, uuid.UUID, []v2input.Entry, v2input.SessionVersionBinding) ([]v2input.Entry, error) {
	p.calls++
	if p.calls == 2 {
		return p.entries[:1], nil
	}
	return p.entries, nil
}

func (p *renderBatchRecoveryProbe) BeginBatch(_ context.Context, _ uuid.UUID, batch, project, snapshot uuid.UUID, who auth.Identity, policy string, parts []CompiledRenderBatchPart) error {
	p.begins++
	if p.failBegin {
		return v2input.ErrExpired
	}
	raw, err := BuildRenderBatchManifest(batch, project, snapshot, who, policy, parts)
	if err != nil {
		return err
	}
	sha, err := canonical.Hash(json.RawMessage(raw))
	if err != nil {
		return err
	}
	manifest, err := DecodeRenderBatchManifest(batch, who.SessionID, raw, sha)
	if err != nil {
		return err
	}
	p.parent = RecoveredRenderBatch{BatchID: batch, Manifest: manifest, ManifestSHA: sha}
	p.parts = make([]RecoveredRenderDraft, len(parts))
	for i, part := range parts {
		p.parts[i] = RecoveredRenderDraft{RunID: part.RunID, WorkflowID: part.Plan.Workflow, ProjectID: project, SnapshotID: snapshot, Config: append(json.RawMessage(nil), part.Plan.Config...), ConfigSHA: part.Plan.ConfigSHA, PolicySHA: policy, ReservedBytes: part.Plan.OutputBytes}
	}
	if p.cancelBegin != nil {
		p.cancelBegin()
	}
	return nil
}
