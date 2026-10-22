package v2render

import (
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// MaterializeAuthoredSegmentParams consumes the locked published prefix. The
// current layer and transformed protection references must already be physically
// published through their scoped authority; this pure compiler cannot promote a
// ready artifact, invent a style.resolve receipt, or move a prior user decision.
// The mounted segment-body scope must repeat all these occurrence bindings.
func MaterializeAuthoredSegmentParams(inventory AuthoredSegmentInventory,currentLayers,protected *v2input.Ref)(CompiledAuthoredSegmentParams,error){
 fail:=func()(CompiledAuthoredSegmentParams,error){return CompiledAuthoredSegmentParams{},v2input.ErrInvalid}
 s:=inventory.Context
 if s.Instruction.Kind!="segment"||len(s.Instruction.Layers)!=1||s.Instruction.Ordinal<1||s.Instruction.Ordinal>4998||len(inventory.Phases)!=s.Instruction.Ordinal-1||!authoredBindingValid(s.Source)||!validLayerArtifactRef(inventory.BaseReceipt){return fail()}
 var stack struct{Layers []json.RawMessage `json:"layers"`}
 if json.Unmarshal([]byte(s.LayerStackJCS),&stack)!=nil{return fail()}
 count:=0;var original struct{ID uuid.UUID `json:"id"`;Enabled bool `json:"enabled"`;Opacity float64 `json:"opacity"`}
 for _,raw:=range stack.Layers{var identity struct{ID uuid.UUID `json:"id"`};if json.Unmarshal(raw,&identity)!=nil{return fail()};if identity.ID==s.Instruction.Layers[0]{count++;if json.Unmarshal(raw,&original)!=nil{return fail()}}}
 if count!=1||original.Opacity<0||original.Opacity>1{return fail()}
 noop:=!original.Enabled||original.Opacity==0
 if noop!=(currentLayers==nil){return fail()}
 refs:=make([]v2input.Ref,len(inventory.Phases))
 for i,phase:=range inventory.Phases{if phase.Ordinal!=i+1||!validLayerArtifactRef(phase.Receipt){return fail()};refs[i]=phase.Receipt}
 return CompileAuthoredSegmentParams(AuthoredSegmentParams{SchemaVersion:1,TenantID:s.Tenant,ParentRunID:s.Parent,WorkflowID:s.Workflow,InstructionID:s.Instruction.Identity,Ordinal:s.Instruction.Ordinal,ProgramSHA256:s.ProgramSHA,PredecessorSHA256:s.PredecessorSHA,Recipe:s.OriginalRecipe,BaseReceipt:inventory.BaseReceipt,Source:s.Source,CurrentLayers:currentLayers,ProtectedMask:protected,PhaseReceipts:refs})
}
