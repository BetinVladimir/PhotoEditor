package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// AuthoredGeometryIntent is an immutable phase boundary, not an admitted task.
// LayerJCS retains mask/opacity/blend and all original Params occurrences. The
// compiler never disables geometry layers in the frozen original recipe.
type AuthoredGeometryIntent struct {
	Ordinal int `json:"ordinal"`
	LayerID uuid.UUID `json:"layer_id"`
	Operation string `json:"operation"`
	OperationVersion string `json:"operation_version"`
	Params v2input.Ref `json:"authored_params"`
	LayerJCS json.RawMessage `json:"layer_jcs"`
	PrecedingSameCanvasLayers []uuid.UUID `json:"preceding_same_canvas_layers"`
}

type AuthoredGeometryPlan struct {
	Recipe v2input.Ref `json:"recipe"`
	OriginalSource v2input.SessionVersionBinding `json:"original_source"`
	Stages []AuthoredGeometryIntent `json:"stages"`
	FinalSameCanvasLayers []uuid.UUID `json:"final_same_canvas_layers"`
}

func authoredGeometryOperation(operation string) bool {
	switch operation {
	case "optical.lens", "optical.perspective", "optical.panorama", "optical.focus_stack", "image.upscale":
		return true
	default:
		return false
	}
}

// CompileAuthoredGeometryPlan reuses the real full-recipe, LayerStack and direct
// document validators. It plans no fabricated output refs or accepted receipts.
// Stage materialization must use actual canonical predecessor outputs, retain
// source occurrences and publish each independently compiled Params/closure.
func CompileAuthoredGeometryPlan(request RenderDraftRequest) (AuthoredGeometryPlan,error) {
	invalid:=func()(AuthoredGeometryPlan,error){return AuthoredGeometryPlan{},v2input.ErrInvalid}
	evidence,err:=CompileRenderEvidence(request.Binding,request.RecipeRef,request.RecipeRaw,request.SourceRaw)
	if err!=nil{return AuthoredGeometryPlan{},err}
	if err=ValidateRenderProtectionEvidence(evidence,request.ProtectionRaw);err!=nil{return AuthoredGeometryPlan{},err}
	if err=ValidateRenderLayerDocuments(evidence,request.LayerRaw,request.LayerDocuments);err!=nil{return AuthoredGeometryPlan{},err}
	plan:=AuthoredGeometryPlan{Recipe:request.RecipeRef,OriginalSource:request.Binding,
		Stages:[]AuthoredGeometryIntent{},FinalSameCanvasLayers:[]uuid.UUID{}}
	if len(request.LayerRaw)==0{return plan,nil}
	var stack struct {
		Source v2input.SessionVersionBinding `json:"source"`
		Layers []json.RawMessage `json:"layers"`
	}
	if json.Unmarshal(request.LayerRaw,&stack)!=nil || stack.Source!=request.Binding{return invalid()}
	pending:=[]uuid.UUID{}
	for ordinal,raw:=range stack.Layers {
		var layer struct {
			ID uuid.UUID `json:"id"`
			Enabled bool `json:"enabled"`
			Opacity float64 `json:"opacity"`
			Operation string `json:"operation"`
			Version string `json:"operation_version"`
			Params v2input.Ref `json:"params"`
		}
		if json.Unmarshal(raw,&layer)!=nil{return invalid()}
		if !layer.Enabled || layer.Opacity==0 || !authoredGeometryOperation(layer.Operation){
			pending=append(pending,layer.ID)
			continue
		}
		if layer.Version!="1"{return invalid()}
		wire,err:=canonical.Bytes(raw);if err!=nil{return invalid()}
		plan.Stages=append(plan.Stages,AuthoredGeometryIntent{
			Ordinal:ordinal,LayerID:layer.ID,Operation:layer.Operation,OperationVersion:layer.Version,
			Params:layer.Params,LayerJCS:append(json.RawMessage(nil),wire...),
			PrecedingSameCanvasLayers:append([]uuid.UUID{},pending...)})
		pending=[]uuid.UUID{}
	}
	plan.FinalSameCanvasLayers=append([]uuid.UUID{},pending...)
	return plan,nil
}
