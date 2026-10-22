package v2render

import (
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// AuthoredGeometryMaterialization retains the exact authored layer, including
// opacity, blend, mask and generated/protected policy. Those settings are not
// folded into the native geometry proposal or silently removed during rebasing.
type AuthoredGeometryMaterialization struct {
 Instruction AuthoredGeometryInstruction
 Operation string
 LayerJCS json.RawMessage
 Params MaterializedAuthoredGeometry
}

// MaterializeAuthoredInstruction selects its operation and Params occurrence
// from the original immutable LayerStack. A caller cannot pick a different
// operation from the allowed set or replace another panorama/focus source.
func MaterializeAuthoredInstruction(source AuthoredInstructionSource,authoredParams []byte)(AuthoredGeometryMaterialization,error){
 fail:=func()(AuthoredGeometryMaterialization,error){return AuthoredGeometryMaterialization{},v2input.ErrInvalid}
 if source.Instruction.Kind!="geometry"||source.Instruction.GeometryLayer==nil||*source.Instruction.GeometryLayer==uuid.Nil{return fail()}
 var stack struct{Source v2input.SessionVersionBinding `json:"source"`;Layers []json.RawMessage `json:"layers"`}
 if json.Unmarshal([]byte(source.LayerStackJCS),&stack)!=nil||!authoredBindingValid(stack.Source)||stack.Source.AssetVersionID!=source.Source.AssetVersionID{return fail()}
 matches:=0;var layerRaw json.RawMessage;var layer struct{
  ID uuid.UUID `json:"id"`;Enabled bool `json:"enabled"`;Opacity float64 `json:"opacity"`
  Operation string `json:"operation"`;Version string `json:"operation_version"`;Params v2input.Ref `json:"params"`
 }
 for _,raw:=range stack.Layers{var identity struct{ID uuid.UUID `json:"id"`};if json.Unmarshal(raw,&identity)!=nil{return fail()};if identity.ID==*source.Instruction.GeometryLayer{matches++;layerRaw=raw}}
 if matches!=1||json.Unmarshal(layerRaw,&layer)!=nil||!layer.Enabled||layer.Opacity<=0||layer.Opacity>1||layer.Version!="1"{return fail()}
 materialized,err:=MaterializeAuthoredGeometry(layer.Operation,layer.Params,authoredParams,stack.Source,source.Source);if err!=nil{return fail()}
 wire,err:=canonical.Bytes(layerRaw);if err!=nil{return fail()}
 return AuthoredGeometryMaterialization{Instruction:source.Instruction,Operation:layer.Operation,LayerJCS:wire,Params:materialized},nil
}
