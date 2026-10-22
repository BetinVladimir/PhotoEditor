package run

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/styleplan"
	"photoeditor/backend/internal/v2render"
)

// PreparedAuthoredParent is not a queued receipt. Only the locked parent
// admission transaction may retain it and allocate quota. No future output
// ArtifactRefs or accepted human decisions occur in this representation.
type PreparedAuthoredParent struct {
	Parent, Workflow uuid.UUID
	RequestSHA       string
	Key              string
	Intent           EditorEffectRequest
	Program          v2render.AuthoredGeometryProgram
	ProgramJCS       json.RawMessage
	ProgramSHA       string
	BasePlan         styleplan.AuthoredBasePlan
	BaseStyleVersion uuid.UUID
	EffectiveStyle   json.RawMessage
	Render           v2render.CalibratedAuthoredRequest
	mode             *editorEffectApplyMode
}

// prepareAuthoredParent resolves both independent closures: the complete Apply
// base (including session/learning/overrides) and original authored layer graph.
// The actual Editor loader is shared, not replaced by a reduced identity style.
func (s EditorEffectsService) prepareAuthoredParent(ctx context.Context, who auth.Identity, tid, pid uuid.UUID, key string, req EditorEffectRequest) (PreparedAuthoredParent, error) {
	invalid := func() (PreparedAuthoredParent, error) {
		return PreparedAuthoredParent{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if tid == uuid.Nil || pid == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) || req.DraftRevision < 1 || req.DraftRevision > 9007199254740991 || req.FrozenDocumentID == uuid.Nil || req.DraftDocumentID == uuid.Nil {
		return invalid()
	}
	if !s.FreshRenderInstalled || s.AuthoredCalibrations == nil {
		return PreparedAuthoredParent{}, domain.NewError("RENDER_UNAVAILABLE", 503)
	}
	if s.Runs.DB == nil || s.Drafts.DB == nil || s.Drafts.Objects == nil {
		return PreparedAuthoredParent{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	// Namespace is deliberately distinct from ordinary Apply and render batches;
	// the full request hash remains an independent SQL collision/replay condition.
	parent := uuid.NewSHA1(tid, []byte("V26.AuthoredRender:"+who.UserID.String()+":"+pid.String()+":"+key))
	workflowID := uuid.NewSHA1(parent, []byte("workflow"))
	requestSHA, err := canonical.Hash(req)
	if err != nil {
		return invalid()
	}
	prepared, err := s.prepareEditorEffects(ctx, who, tid, pid, req, nil, true)
	if err != nil {
		return PreparedAuthoredParent{}, err
	}
	render, err := v2render.LoadCalibratedAuthoredRequest(ctx, s.Runs.DB, s.Drafts.Objects, s.Drafts.Bucket, s.AuthoredCalibrations, tid, pid, who, v2render.RenderSelectionItem{Binding: req.Source, Recipe: req.Recipe})
	if err != nil {
		return PreparedAuthoredParent{}, err
	}
	// Both concrete loaders must have observed the same immutable frozen bytes;
	// replacing the recipe or its original source during IO is never tolerated.
	if !bytes.Equal(prepared.FrozenRaw, render.Request.RecipeRaw) || render.Request.Binding != req.Source || render.Request.RecipeRef != req.Recipe {
		return invalid()
	}
	program, err := v2render.CompileAuthoredGeometryProgram(workflowID, render.Request)
	if err != nil {
		return PreparedAuthoredParent{}, err
	}
	programJCS, err := canonical.Marshal(program)
	if err != nil {
		return invalid()
	}
	programSHA, err := canonical.Hash(program)
	if err != nil {
		return invalid()
	}
	style, err := canonical.Marshal(prepared.Compilation.Style)
	if err != nil {
		return invalid()
	}
	basePlan, err := styleplan.CompileAuthoredBasePlan(style, 1)
	if err != nil {
		return PreparedAuthoredParent{}, err
	}
	if prepared.Mode.Precompose || prepared.Mode.AuthoredBase != nil || prepared.Mode.ExecutedStyle.ArtifactID == uuid.Nil {
		return invalid()
	}
	if err = ctx.Err(); err != nil {
		return PreparedAuthoredParent{}, err
	}
	return PreparedAuthoredParent{Parent: parent, Workflow: workflowID, RequestSHA: requestSHA, Key: key, Intent: req, Program: program, ProgramJCS: programJCS, ProgramSHA: programSHA, BasePlan: basePlan, BaseStyleVersion: prepared.StyleVersion, EffectiveStyle: style, Render: render, mode: prepared.Mode}, nil
}
