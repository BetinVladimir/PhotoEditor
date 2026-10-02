package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// RecoverInputs resumes publication/registration for an existing immutable
// draft. It never recompiles runtime choices, creates a run or activates tasks.
// SQL publication and registration must still repeat current draft authority.
func (c RenderDraftCoordinator) RecoverInputs(ctx context.Context, tenant uuid.UUID, who auth.Identity, saved RecoveredRenderDraft) error {
	nodes, drafts, err := c.prepareRecoveredInputs(ctx, tenant, who, saved)
	if err != nil {
		return err
	}
	return c.publishRecoveredInputs(ctx, tenant, who, saved, nodes, drafts)
}

func (c RenderDraftCoordinator) prepareRecoveredInputs(ctx context.Context, tenant uuid.UUID, who auth.Identity, saved RecoveredRenderDraft) ([]RecoveredRenderNode, []v2input.DraftTask, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || c.Inputs == nil || c.Transport == nil || c.Resolver == nil {
		return nil, nil, v2input.ErrInvalid
	}
	nodes, err := DecodeRecoveredRenderPlan(saved)
	if err != nil {
		return nil, nil, err
	}
	var config struct {
		Render struct {
			Session uuid.UUID `json:"auth_session_id"`
		} `json:"v26_render"`
	}
	if json.Unmarshal(saved.Config, &config) != nil || config.Render.Session != who.SessionID {
		return nil, nil, v2input.ErrInvalid
	}
	drafts := make([]v2input.DraftTask, len(nodes))
	// Re-resolve every scoped inventory before any object publication. Drift in
	// one node must not publish another node against a changed saved plan.
	for i, recovered := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		node := recovered.Node
		entries, err := ResolveRenderInputs(ctx, who, tenant, recovered.Evidence.Recipe.Scalars.Source, node.Params, []byte(node.ParamsJCS), []byte(node.SourceJCS), c.Resolver)
		if err != nil {
			return nil, nil, err
		}
		digest, err := canonical.Hash(struct {
			Task      uuid.UUID       `json:"task"`
			Operation string          `json:"operation"`
			Version   string          `json:"version"`
			Entries   []v2input.Entry `json:"entries"`
		}{node.TaskID, "render.compose", "1", entries})
		if err != nil || digest != node.InputDigest {
			return nil, nil, v2input.ErrInvalid
		}
		key, err := v2input.NodeKey("render.compose", i)
		if err != nil {
			return nil, nil, err
		}
		drafts[i] = v2input.DraftTask{ID: node.TaskID, NodeKey: key, Operation: "render.compose", OperationVersion: "1", ResourceClass: "gpu", ParamsArtifactID: node.Params.ArtifactID, ParamsSHA: node.Params.SHA256, InputDigest: node.InputDigest, ExecutionProfile: append(json.RawMessage(nil), node.Profile...)}
	}
	return nodes, drafts, nil
}

func (c RenderDraftCoordinator) publishRecoveredInputs(ctx context.Context, tenant uuid.UUID, who auth.Identity, saved RecoveredRenderDraft, nodes []RecoveredRenderNode, drafts []v2input.DraftTask) error {
	for i, recovered := range nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		node := recovered.Node
		graph, err := c.Transport.Prepare(ctx, who, tenant, saved.RunID, node.TaskID, recovered.Evidence.Recipe.Scalars.Source, node.Params, []byte(node.ParamsJCS), []byte(node.SourceJCS), c.Resolver)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.Inputs.RegisterRenderInputs(ctx, tenant, saved.RunID, saved.WorkflowID, i, drafts[i], graph); err != nil {
			return err
		}
	}
	return ctx.Err()
}
