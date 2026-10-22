package workflow

import (
	"photoeditor/backend/internal/media"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// MultiPreviewPlan is an authored plan, not a reservation or admitted graph.
type MultiPreviewPlan struct {
	source    v2render.MultiPreviewSource
	run       uuid.UUID
	srgb      string
	policy    string
	documents []RenderDerivativeDocument
}

func (p MultiPreviewPlan) Run() uuid.UUID                      { return p.run }
func (p MultiPreviewPlan) Source() v2render.MultiPreviewSource { return p.source }
func (p MultiPreviewPlan) SRGBSHA() string                     { return p.srgb }
func (p MultiPreviewPlan) Documents() []RenderDerivativeDocument {
	out := make([]RenderDerivativeDocument, len(p.documents))
	for i, d := range p.documents {
		out[i] = d
		out[i].Params = append(json.RawMessage(nil), d.Params...)
		out[i].Profile = append(json.RawMessage(nil), d.Profile...)
		out[i].Inputs = append(d.Inputs[:0:0], d.Inputs...)
	}
	return out
}
func CompileMultiPreviewPlan(source v2render.MultiPreviewSource, content v2render.MultiResultContent, srgb string, policies ...string) (MultiPreviewPlan, error) {
	policy,policyErr:=media.ResolveRendererPolicy(policies...);if policyErr!=nil{return MultiPreviewPlan{},policyErr}
	run, err := v2render.MultiPreviewRunID(source, srgb)
	if err != nil {
		return MultiPreviewPlan{}, err
	}
	root, image, err := content.PreviewDocuments(source)
	if err != nil {
		return MultiPreviewPlan{}, err
	}
	intent := v2render.RenderDerivativeIntent{Tenant: source.Tenant, Project: source.Project, Run: run, Task: source.Producer, Result: source.Binding.ResultID, Root: source.Binding.Result, SourceAttempt: source.Attempt, SourceFence: source.Fence, SRGBSHA: srgb}
	intent.RendererPolicy=policy
	docs, err := BuildRenderDerivativeDocuments(intent, root, image)
	if err != nil {
		return MultiPreviewPlan{}, err
	}
	plan := MultiPreviewPlan{source: source, run: run, srgb: srgb, policy:policy, documents: docs}
	if err := validateMultiPreviewPlan(plan); err != nil {
		return MultiPreviewPlan{}, err
	}
	return plan, nil
}
func validateMultiPreviewPlan(p MultiPreviewPlan) error {
	expected, err := v2render.MultiPreviewRunID(p.source, p.srgb)
	if err != nil || p.run != expected || len(p.documents) != 2 {
		return v2input.ErrInvalid
	}
	for i, variant := range []string{"preview1600", "preview2560"} {
		d := p.documents[i]
		if d.Run != expected || d.ID != uuid.NewSHA1(expected, []byte("render.derivative:"+variant)) || d.Variant != variant || renderDerivativeBytesSHA(d.Params) != d.ParamsSHA {
			return v2input.ErrInvalid
		}
	}
	return nil
}
