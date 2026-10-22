package v2render

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ResumeOriginal derives the style claim and all five witnesses from actual
// scoped immutable documents and the current segment qualification selection.
// It never accepts a caller-selected effective configuration or rewritten
// ResolvedStyle. Runtime compute/adoption independently repeat the algorithm.
func (s SQLAuthoredDetailMaterialization) ResumeOriginal(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int,
	transformTasks []uuid.UUID, protected *v2input.Ref) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.Original.API == nil || s.Continuation.Style.Scheduler == nil {
		return false, v2input.ErrInvalid
	}
	source, current, err := ReadAuthoredInstructionSource(ctx, s.Continuation.Style.Scheduler, tenant, parent, ordinal)
	if err != nil {
		return false, err
	}
	if !current {
		return false, v2input.ErrExpired
	}
	if source.Actor != who.UserID || source.Session != who.SessionID || source.Instruction.Kind != "segment" || len(source.Instruction.Layers) != 1 {
		return false, v2input.ErrInvalid
	}
	var stack struct {
		Layers []struct {
			ID        uuid.UUID   `json:"id"`
			Operation string      `json:"operation"`
			Version   string      `json:"operation_version"`
			Params    v2input.Ref `json:"params"`
		} `json:"layers"`
	}
	if json.Unmarshal([]byte(source.LayerStackJCS), &stack) != nil {
		return false, v2input.ErrInvalid
	}
	paramsRef := v2input.Ref{}
	count := 0
	for _, layer := range stack.Layers {
		if layer.ID == source.Instruction.Layers[0] {
			if layer.Operation != "portrait.detail" || layer.Version != "1" {
				return false, v2input.ErrInvalid
			}
			paramsRef = layer.Params
			count++
		}
	}
	if count != 1 || !validLayerArtifactRef(paramsRef) {
		return false, v2input.ErrInvalid
	}
	read := func(ref v2input.Ref) ([]byte, error) {
		return s.Original.ReadJSON(ctx, who, tenant, source.Project, parent, ordinal, ref)
	}
	paramsRaw, err := read(paramsRef)
	if err != nil {
		return false, err
	}
	var params struct {
		Image    v2input.Ref `json:"image"`
		Resolved v2input.Ref `json:"resolved"`
	}
	if json.Unmarshal(paramsRaw, &params) != nil || !validLayerArtifactRef(params.Image) || !validLayerArtifactRef(params.Resolved) {
		return false, v2input.ErrInvalid
	}
	var recipe struct {
		Recipe struct {
			Source v2input.SessionVersionBinding `json:"source"`
		} `json:"recipe"`
	}
	if json.Unmarshal([]byte(source.RecipeJCS), &recipe) != nil || !authoredBindingValid(recipe.Recipe.Source) {
		return false, v2input.ErrInvalid
	}
	originalRaw, err := read(recipe.Recipe.Source.Source)
	if err != nil {
		return false, err
	}
	authoredRaw, err := read(params.Image)
	if err != nil {
		return false, err
	}
	currentRaw, err := read(source.Source.Source)
	if err != nil {
		return false, err
	}
	resolvedRaw, err := read(params.Resolved)
	if err != nil {
		return false, err
	}
	effective, err := CompileAuthoredEffectiveParams(resolvedRaw)
	if err != nil {
		return false, err
	}
	original, err := review.ParseWorkingImage(originalRaw)
	if err != nil {
		return false, err
	}
	authored, err := review.ParseWorkingImage(authoredRaw)
	if err != nil {
		return false, err
	}
	target, err := review.ParseWorkingImage(currentRaw)
	if err != nil {
		return false, err
	}
	if original.Color != authored.Color || original.Color != target.Color || original.Canvas.SourceSHA256 != authored.Canvas.SourceSHA256 || original.Canvas.SourceSHA256 != target.Canvas.SourceSHA256 || recipe.Recipe.Source.AssetVersionID != source.Source.AssetVersionID {
		return false, v2input.ErrInvalid
	}
	var resolved struct {
		Params struct {
			Image v2input.Ref `json:"image"`
		} `json:"params"`
	}
	if json.Unmarshal(resolvedRaw, &resolved) != nil || resolved.Params.Image != params.Image {
		return false, v2input.ErrInvalid
	}
	qualification, err := (SQLAuthoredSegmentQualificationResolver{API: s.Original.API}).Resolve(ctx, who, tenant, source.Project)
	if err != nil {
		return false, err
	}
	canvas, err := canonical.Marshal(authored.Canvas)
	if err != nil {
		return false, err
	}
	authoredSource := v2input.SessionVersionBinding{AssetVersionID: recipe.Recipe.Source.AssetVersionID, Source: params.Image, CanvasHash: authoredDetailSHA(canvas)}
	body, err := canonical.Marshal(map[string]any{"schema_version": 1, "operation": "portrait.detail", "operation_version": "1",
		"tenant_id": tenant, "parent_run_id": parent, "workflow_id": source.Workflow, "instruction_id": source.Instruction.Identity, "layer_id": source.Instruction.Layers[0],
		"original_recipe": source.OriginalRecipe, "authored_params": paramsRef, "authored_resolved": params.Resolved, "original_source": recipe.Recipe.Source, "authored_source": authoredSource, "current_source": source.Source,
		"program_sha256": source.ProgramSHA, "predecessor_sha256": source.PredecessorSHA, "effective_sha256": authoredDetailSHA(effective), "runtime_sha256": qualification.RuntimeSHA})
	if err != nil {
		return false, err
	}
	witnesses := map[string]string{"authored_image_jcs": string(authoredRaw), "authored_params_jcs": string(paramsRaw), "current_image_jcs": string(currentRaw), "original_image_jcs": string(originalRaw), "resolved_jcs": string(resolvedRaw)}
	return s.resumeScopedDetail(ctx, who, source, body, witnesses, paramsRaw, currentRaw, transformTasks, protected)
}
