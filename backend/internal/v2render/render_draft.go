package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type RenderDraftRequest struct {
	Tenant                                        uuid.UUID
	Actor                                         auth.Identity
	Binding                                       v2input.SessionVersionBinding
	RecipeRef                                     v2input.Ref
	RecipeRaw, SourceRaw, LayerRaw, ProtectionRaw []byte
	ClosureDocuments                              []v2draft.DocumentWitness
	ClosureOccurrences                            []v2draft.OccurrenceWitness
	LayerDocuments                                []RenderLayerDocument
	Backend, RuntimeSHA, AlphaMode                string
	TileSize                                      int
	Provider                                      RenderProviderPeak
}

type renderResolvedInventory struct{ entries []v2input.Entry }

func (r renderResolvedInventory) ResolveTaskInputs(context.Context, auth.Identity, uuid.UUID, []v2input.Entry, v2input.SessionVersionBinding) ([]v2input.Entry, error) {
	return r.entries, nil
}

// CompileRenderDraft freezes a task shell from a complete scoped inventory.
// It revalidates byte/binding/profile coverage but does not independently prove
// SQL authority, qualification or recursively discover omitted descendants.
func CompileRenderDraft(workflow uuid.UUID, ordinal int, request RenderDraftRequest, resolved []v2input.Entry) (v2input.DraftTask, v2input.CursorFactory, error) {
	evidence, err := CompileRenderEvidence(request.Binding, request.RecipeRef, request.RecipeRaw, request.SourceRaw)
	if err != nil {
		return v2input.DraftTask{}, nil, err
	}
	if err := ValidateRenderLayerDocuments(evidence, request.LayerRaw, request.LayerDocuments); err != nil {
		return v2input.DraftTask{}, nil, err
	}
	task, err := v2input.TaskID(workflow, "render.compose", ordinal)
	if err != nil {
		return v2input.DraftTask{}, nil, err
	}
	key, err := v2input.NodeKey("render.compose", ordinal)
	if err != nil {
		return v2input.DraftTask{}, nil, err
	}
	entries, err := ResolveRenderInputs(context.Background(), request.Actor, request.Tenant, request.Binding,
		request.RecipeRef, request.RecipeRaw, request.SourceRaw, renderResolvedInventory{resolved})
	if err != nil {
		return v2input.DraftTask{}, nil, err
	}
	if err := validateRenderLayerInventory(request.LayerDocuments, entries); err != nil {
		return v2input.DraftTask{}, nil, err
	}
	if err := validateRenderClosureDocuments(request.ClosureDocuments, entries); err != nil {
		return v2input.DraftTask{}, nil, err
	}
	if err := validateRenderClosureCoverage(request); err != nil {
		return v2input.DraftTask{}, nil, err
	}
	if err := validateRenderOccurrences(request, entries); err != nil {
		return v2input.DraftTask{}, nil, err
	}
	profile, err := CompileRenderExecutionProfile(request.Binding, request.RecipeRef, request.RecipeRaw,
		request.SourceRaw, request.LayerRaw, request.Backend, request.RuntimeSHA, request.TileSize, request.AlphaMode, request.Provider)
	if err != nil {
		return v2input.DraftTask{}, nil, err
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task, "render.compose", "1", entries})
	if err != nil {
		return v2input.DraftTask{}, nil, err
	}
	open := func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &compositionCursor{entries: entries}, nil
	}
	return v2input.DraftTask{ID: task, NodeKey: key, Operation: "render.compose", OperationVersion: "1",
		ResourceClass: "gpu", ParamsArtifactID: request.RecipeRef.ArtifactID, ParamsSHA: request.RecipeRef.SHA256,
		InputDigest: digest, ExecutionProfile: profile}, open, nil
}
