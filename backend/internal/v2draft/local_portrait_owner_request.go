package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// LocalPortraitOwnerRequest is immutable request content to persist before
// reserving Params. SQL must verify every scope pin under locks before storing it.
type LocalPortraitOwnerRequest struct {
	SchemaVersion  int                 `json:"schema_version"`
	Tenant         uuid.UUID           `json:"tenant_id"`
	Project        uuid.UUID           `json:"project_id"`
	Snapshot       uuid.UUID           `json:"snapshot_id"`
	Actor          uuid.UUID           `json:"actor_id"`
	Session        uuid.UUID           `json:"auth_session_id"`
	Run            uuid.UUID           `json:"run_id"`
	Workflow       uuid.UUID           `json:"workflow_id"`
	Ordinal        int                 `json:"ordinal"`
	Width          int64               `json:"width"`
	Height         int64               `json:"height"`
	ManualRoot     v2input.Ref         `json:"manual_root"`
	OriginalParams v2input.Ref         `json:"original_params"`
	TargetPixels   v2input.Ref         `json:"target_pixels"`
	GuardPixels    v2input.Ref         `json:"guard_pixels"`
	Vectors        *v2input.Ref        `json:"vectors"`
	Intent         LocalPortraitIntent `json:"intent"`
}

func (s ClosureResolver) CompileLocalPortraitOwnerRequest(ctx context.Context, who auth.Identity,
	tenant, project, snapshot, run uuid.UUID, ordinal int, operation string, binding v2input.SessionVersionBinding,
	sourceRaw, paramsRaw []byte, runtimeSHA string, capacity v2portrait.LocalResourceEstimate,
	proof LocalPortraitRecoveryProof) (json.RawMessage, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return nil, "", v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return nil, "", err
	}
	sourceRaw = append([]byte(nil), sourceRaw...)
	proof.RootRaw = append([]byte(nil), proof.RootRaw...)
	proof.OriginalRaw = append([]byte(nil), proof.OriginalRaw...)
	proof.VectorsRaw = append([]byte(nil), proof.VectorsRaw...)
	intent, err := s.CompileLocalPortraitIntent(ctx, who, tenant, workflow, ordinal, operation, binding, sourceRaw, paramsRaw, runtimeSHA, capacity, proof)
	if err != nil {
		return nil, "", err
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return nil, "", err
	}
	root, err := review.ParseManualRasterDocument(proof.RootRaw, review.ArtifactRef{ID: proof.Root.ArtifactID, SHA256: proof.Root.SHA256})
	if err != nil {
		return nil, "", err
	}
	vectors, err := CompileManualRasterVectors(binding.Source, sourceRaw, proof.Root, proof.RootRaw, proof.OriginalParams, proof.OriginalRaw, proof.VectorsRaw)
	if err != nil {
		return nil, "", err
	}
	request := LocalPortraitOwnerRequest{SchemaVersion: 1, Tenant: tenant, Project: project, Snapshot: snapshot, Actor: who.UserID, Session: who.SessionID,
		Run: run, Workflow: workflow, Ordinal: ordinal, Width: source.Canvas.Width, Height: source.Canvas.Height,
		ManualRoot: proof.Root, OriginalParams: proof.OriginalParams, TargetPixels: v2input.Ref{ArtifactID: root.Target.Artifact.ID, SHA256: root.Target.Artifact.SHA256},
		GuardPixels: v2input.Ref{ArtifactID: root.Protected.Artifact.ID, SHA256: root.Protected.Artifact.SHA256}, Intent: intent}
	if vectors.Contract == "ManualMask" {
		ref := vectors.Ref
		request.Vectors = &ref
	}
	body, err := canonical.Marshal(request)
	if err != nil || len(body) > 1048576 {
		return nil, "", v2input.ErrInvalid
	}
	hash, err := canonical.Hash(request)
	if err != nil {
		return nil, "", err
	}
	if err = ctx.Err(); err != nil {
		return nil, "", err
	}
	return body, hash, nil
}
