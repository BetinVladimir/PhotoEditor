package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// PerspectivePreviewPlan retains independently authorized provenance alongside
// media parameters. It is not a persisted run, reservation or admitted graph.
type PerspectivePreviewPlan struct {
	source    v2render.PerspectivePreviewSource
	run       uuid.UUID
	srgb      string
	documents []RenderDerivativeDocument
}

func (p PerspectivePreviewPlan) Run() uuid.UUID                            { return p.run }
func (p PerspectivePreviewPlan) Source() v2render.PerspectivePreviewSource { return p.source }
func (p PerspectivePreviewPlan) SRGBSHA() string                           { return p.srgb }
func (p PerspectivePreviewPlan) Documents() []RenderDerivativeDocument {
	out := make([]RenderDerivativeDocument, len(p.documents))
	for i, d := range p.documents {
		out[i] = d
		out[i].Params = append(json.RawMessage(nil), d.Params...)
		out[i].Profile = append(json.RawMessage(nil), d.Profile...)
		out[i].Inputs = append(out[i].Inputs[:0:0], d.Inputs...)
	}
	return out
}

func CompilePerspectivePreviewPlan(bundle v2render.PerspectivePreviewBundle, srgb string) (PerspectivePreviewPlan, error) {
	var zero PerspectivePreviewPlan
	pin := bundle.Source()
	if _, err := domain.ParseDigest(srgb); err != nil {
		return zero, v2input.ErrInvalid
	}
	if pin.Tenant == uuid.Nil || pin.Project == uuid.Nil || pin.Run == uuid.Nil || pin.ResultID == uuid.Nil || pin.Result.ArtifactID == uuid.Nil || pin.Attempt < 1 || pin.Fence < 1 {
		return zero, v2input.ErrInvalid
	}
	if err := v2render.ValidatePerspectivePreviewSource(pin); err != nil {
		return zero, v2input.ErrInvalid
	}
	originals := bundle.Documents()
	rootRaw := originals[pin.Result.ArtifactID.String()]
	result, err := v2draft.ParseImageResult(rootRaw, pin.Result)
	if err != nil {
		return zero, err
	}
	// A distinct deterministic preview run never mutates the waiting-review parent.
	run := uuid.NewSHA1(pin.Run, []byte("V29.PerspectivePreview:"+pin.Producer.String()+":"+pin.ResultID.String()+":"+pin.Result.SHA256+":"+srgb))
	intent := v2render.RenderDerivativeIntent{Tenant: pin.Tenant, Run: run, Project: pin.Project, Task: pin.Producer, Result: pin.ResultID, Root: pin.Result, SourceAttempt: pin.Attempt, SourceFence: pin.Fence, SRGBSHA: srgb}
	documents, err := BuildRenderDerivativeDocuments(intent, rootRaw, originals[result.WorkingImage.ArtifactID.String()])
	if err != nil {
		return zero, err
	}
	return PerspectivePreviewPlan{source: pin, run: run, srgb: srgb, documents: documents}, nil
}
