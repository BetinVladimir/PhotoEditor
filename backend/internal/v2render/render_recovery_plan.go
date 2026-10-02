package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type RecoveredRenderNode struct {
	Node     RenderRunNode
	Evidence CompiledRenderEvidence
}

// DecodeRecoveredRenderPlan validates frozen evidence without granting catalog
// access or provider qualification. Inventories must be freshly scoped before
// publication and matched to each saved input digest.
func DecodeRecoveredRenderPlan(saved RecoveredRenderDraft) ([]RecoveredRenderNode, error) {
	invalid := func() ([]RecoveredRenderNode, error) { return nil, v2input.ErrInvalid }
	workflow, err := v2input.WorkflowID(saved.RunID)
	if err != nil || workflow != saved.WorkflowID || len(saved.Config) < 2 || len(saved.Config) > 1048576 || saved.ReservedBytes < 1 || saved.ReservedBytes > 9007199254740991 {
		return invalid()
	}
	hash, err := canonical.Hash(saved.Config)
	if err != nil || hash != saved.ConfigSHA {
		return invalid()
	}
	var config struct {
		Project  uuid.UUID `json:"project_id"`
		Snapshot uuid.UUID `json:"input_snapshot_id"`
		Render   struct {
			Session uuid.UUID       `json:"auth_session_id"`
			Nodes   []RenderRunNode `json:"nodes"`
		} `json:"v26_render"`
	}
	if json.Unmarshal(saved.Config, &config) != nil || config.Project != saved.ProjectID || config.Snapshot != saved.SnapshotID || config.Project == uuid.Nil || config.Snapshot == uuid.Nil || config.Render.Session == uuid.Nil || len(config.Render.Nodes) < 1 || len(config.Render.Nodes) > 5000 {
		return invalid()
	}
	result := make([]RecoveredRenderNode, 0, len(config.Render.Nodes))
	var output int64
	for ordinal, node := range config.Render.Nodes {
		task, err := v2input.TaskID(workflow, "render.compose", ordinal)
		if err != nil || node.TaskID != task || !validLayerArtifactRef(v2input.Ref{ArtifactID: task, SHA256: node.InputDigest}) || len(node.Profile) > 65536 {
			return invalid()
		}
		recipe, err := v2draft.DecodeFrozenRenderRecipe([]byte(node.ParamsJCS))
		if err != nil {
			return invalid()
		}
		evidence, err := CompileRenderEvidence(recipe.Scalars.Source, node.Params, []byte(node.ParamsJCS), []byte(node.SourceJCS))
		if err != nil {
			return invalid()
		}
		if err := ValidateRenderProtectionEvidence(evidence, []byte(node.ProtectionJCS)); err != nil {
			return invalid()
		}
		var profile struct {
			Backend   string                `json:"backend"`
			Renderer  string                `json:"renderer_version"`
			Runtime   string                `json:"render_runtime_sha"`
			Model     string                `json:"model_lock_sha"`
			ICC       string                `json:"icc_lock_sha"`
			Tile      int                   `json:"tile_size"`
			Alpha     string                `json:"alpha_mode"`
			Resources LayerResourceEstimate `json:"resource_estimate"`
		}
		if json.Unmarshal(node.Profile, &profile) != nil || (profile.Backend != "cpu" && profile.Backend != "cuda" && profile.Backend != "mps") || profile.Renderer != "v2.2" || !validLayerArtifactRef(v2input.Ref{ArtifactID: task, SHA256: profile.Runtime}) || profile.Model != recipe.Scalars.ModelLockSHA || profile.ICC != recipe.Scalars.ICCLockSHA {
			return invalid()
		}
		minimum, err := CompileRenderBudget(recipe.Scalars.Source, node.Params, []byte(node.ParamsJCS), []byte(node.SourceJCS), []byte(node.LayerJCS), profile.Tile, profile.Alpha, RenderProviderPeak{})
		if err != nil || profile.Resources.PixelCount != minimum.Resources.PixelCount || profile.Resources.RAMBytes < minimum.Resources.RAMBytes || profile.Resources.ScratchBytes < minimum.Resources.ScratchBytes || profile.Resources.OutputBytes < minimum.Resources.OutputBytes || profile.Resources.VRAMBytes < 0 {
			return invalid()
		}
		for _, value := range []int64{profile.Resources.RAMBytes, profile.Resources.ScratchBytes, profile.Resources.OutputBytes, profile.Resources.VRAMBytes} {
			if value > 9007199254740991 {
				return invalid()
			}
		}
		if output > saved.ReservedBytes-profile.Resources.OutputBytes {
			return invalid()
		}
		output += profile.Resources.OutputBytes
		node.Profile = append(json.RawMessage(nil), node.Profile...)
		result = append(result, RecoveredRenderNode{node, evidence})
	}
	return result, nil
}
