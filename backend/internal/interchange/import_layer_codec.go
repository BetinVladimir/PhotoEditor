package interchange

import (
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// QualifyImportedLayers checks each operation/version against its complete
// remapped Params, operator/model locks and source geometry. Disabled layers
// remain authored data and must also have valid portable dependencies.
type ImportLayerQualifier interface{QualifyImportedLayers(context.Context,[]byte,v2input.SessionVersionBinding)error}
type ImportLayerStackCodec struct{Qualifier ImportLayerQualifier;Fallback ImportDependencyCodec}
func(c ImportLayerStackCodec)RemapImportDocument(ctx context.Context,contract string,raw []byte,old,target v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if contract!="LayerStack"{if c.Fallback!=nil{return c.Fallback.RemapImportDocument(ctx,contract,raw,old,target,resolve)};return nil,&ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 if c.Qualifier==nil||resolve==nil{return nil,v2input.ErrInvalid}
 if _,err:=v2draft.ValidateLayerStack(raw,old);err!=nil{return nil,err}
 if old.CanvasHash!=target.CanvasHash{return nil,&ImportRecipeUnsupported{Reason:"layer_canvas_remap_required"}}
 var fields map[string]json.RawMessage;if err:=json.Unmarshal(raw,&fields);err!=nil{return nil,err}
 var layers []map[string]json.RawMessage;if err:=json.Unmarshal(fields["layers"],&layers);err!=nil{return nil,err}
 remap:=func(raw json.RawMessage,kind string)(json.RawMessage,error){
  if err:=ctx.Err();err!=nil{return nil,err}
  var ref v2input.Ref;if err:=json.Unmarshal(raw,&ref);err!=nil{return nil,err}
  mapped,err:=resolve(ref,kind);if err!=nil{return nil,err}
  if !validImportRef(mapped)||mapped.ArtifactID==ref.ArtifactID{return nil,v2input.ErrInvalid}
  return canonical.Marshal(mapped)
 }
 nodes:=0
 var mask func(json.RawMessage,int)(json.RawMessage,error)
 mask=func(raw json.RawMessage,depth int)(json.RawMessage,error){
  if err:=ctx.Err();err!=nil{return nil,err};nodes++;if depth>16||nodes>256{return nil,v2input.ErrInvalid}
  var value map[string]json.RawMessage;if err:=json.Unmarshal(raw,&value);err!=nil{return nil,err}
  var op string;if err:=json.Unmarshal(value["op"],&op);err!=nil{return nil,err}
  switch op{
  case "ref":
   mapped,err:=remap(value["mask"],"MaskRef");if err!=nil{return nil,err};value["mask"]=mapped
   value["canvas_hash"],err=canonical.Marshal(target.CanvasHash);if err!=nil{return nil,err}
  case "invert":mapped,err:=mask(value["operand"],depth+1);if err!=nil{return nil,err};value["operand"]=mapped
  case "union","intersect","subtract":
   for _,key:=range []string{"left","right"}{mapped,err:=mask(value[key],depth+1);if err!=nil{return nil,err};value[key]=mapped}
  default:return nil,v2input.ErrInvalid
  }
  return canonical.Marshal(value)
 }
 for _,layer:=range layers{
  if err:=ctx.Err();err!=nil{return nil,err}
  var id uuid.UUID;if err:=json.Unmarshal(layer["id"],&id);err!=nil{return nil,err}
  newID:=uuid.NewSHA1(target.AssetVersionID,[]byte("interchange:layer:"+id.String()))
  var err error
  layer["id"],err=canonical.Marshal(newID);if err!=nil{return nil,err}
  layer["params"],err=remap(layer["params"],"Params");if err!=nil{return nil,err}
  layer["mask"],err=mask(layer["mask"],0);if err!=nil{return nil,err}
 }
 var err error
 fields["source"],err=canonical.Marshal(target);if err!=nil{return nil,err}
 fields["layers"],err=canonical.Marshal(layers);if err!=nil{return nil,err}
 prepared,err:=canonical.Marshal(fields);if err!=nil{return nil,err}
 if err:=c.QualifyRemappedImportDocument(ctx,contract,prepared,target);err!=nil{return nil,err}
 return prepared,ctx.Err()
}
func(c ImportLayerStackCodec)QualifyRemappedImportDocument(ctx context.Context,contract string,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if contract!="LayerStack"{if c.Fallback!=nil{return c.Fallback.QualifyRemappedImportDocument(ctx,contract,raw,target)};return &ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 if c.Qualifier==nil{return v2input.ErrInvalid}
 if _,err:=v2draft.ValidateLayerStack(raw,target);err!=nil{return err}
 if err:=c.Qualifier.QualifyImportedLayers(ctx,append([]byte(nil),raw...),target);err!=nil{return err}
 return ctx.Err()
}

func(c ImportLayerStackCodec)BindImportDocumentSource(source v2input.Ref)ImportDependencyCodec{
 if binder,ok:=c.Fallback.(ImportDocumentSourceBinder);ok{c.Fallback=binder.BindImportDocumentSource(source)}
 return c
}
