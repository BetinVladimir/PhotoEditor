package v2render

import (
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// AuthoredGeometryInstruction deliberately has no future ArtifactRefs. Actual
// task Params are materialized only after the preceding canonical phase commits.
type AuthoredGeometryInstruction struct {
 Identity uuid.UUID `json:"identity"`
 Ordinal int `json:"ordinal"`
 Kind string `json:"kind"`
 Predecessor *uuid.UUID `json:"predecessor"`
 Layers []uuid.UUID `json:"layers"`
 GeometryLayer *uuid.UUID `json:"geometry_layer"`
}
type AuthoredGeometryProgram struct {
 ProgramVersion int `json:"program_version"`
 Workflow uuid.UUID `json:"workflow"`
 FrozenSHA string `json:"frozen_sha256"`
 RecipeSHA string `json:"recipe_sha256"`
 Instructions []AuthoredGeometryInstruction `json:"instructions"`
 RequiresCanonicalExpansion bool `json:"requires_canonical_expansion"`
}
func CompileAuthoredGeometryProgram(workflow uuid.UUID,request RenderDraftRequest)(AuthoredGeometryProgram,error){
 invalid:=func()(AuthoredGeometryProgram,error){return AuthoredGeometryProgram{},v2input.ErrInvalid}
 if workflow==uuid.Nil{return invalid()}
 plan,err:=CompileAuthoredGeometryPlan(request);if err!=nil{return AuthoredGeometryProgram{},err}
 if len(plan.Stages)==0{return invalid()}
 var frozen struct{Recipe json.RawMessage `json:"recipe"`}
 if json.Unmarshal(request.RecipeRaw,&frozen)!=nil||len(frozen.Recipe)==0{return invalid()}
 frozenSHA,err:=canonical.Hash(json.RawMessage(request.RecipeRaw));if err!=nil{return invalid()}
 recipeSHA,err:=canonical.Hash(frozen.Recipe);if err!=nil{return invalid()}
 program:=AuthoredGeometryProgram{ProgramVersion:2,Workflow:workflow,FrozenSHA:frozenSHA,RecipeSHA:recipeSHA,
  Instructions:[]AuthoredGeometryInstruction{},RequiresCanonicalExpansion:true}
 appendStep:=func(kind string,layers []uuid.UUID,geometry *uuid.UUID)error{
  ordinal:=len(program.Instructions);id,e:=v2input.TaskID(workflow,"render.authored_"+kind,ordinal);if e!=nil{return e}
  var previous *uuid.UUID;if ordinal>0{value:=program.Instructions[ordinal-1].Identity;previous=&value}
  program.Instructions=append(program.Instructions,AuthoredGeometryInstruction{Identity:id,Ordinal:ordinal,
   Kind:kind,Predecessor:previous,Layers:append([]uuid.UUID{},layers...),GeometryLayer:geometry});return nil
 }
 if err=appendStep("base",nil,nil);err!=nil{return invalid()}
 for _,stage:=range plan.Stages{
  for _,sameCanvasLayer:=range stage.PrecedingSameCanvasLayers{if err=appendStep("segment",[]uuid.UUID{sameCanvasLayer},nil);err!=nil{return invalid()}}
  layer:=stage.LayerID;if err=appendStep("geometry",nil,&layer);err!=nil{return invalid()}
 }
 for _,sameCanvasLayer:=range plan.FinalSameCanvasLayers{if err=appendStep("segment",[]uuid.UUID{sameCanvasLayer},nil);err!=nil{return invalid()}}
 if err=appendStep("finalize",nil,nil);err!=nil{return invalid()}
 return program,nil
}
