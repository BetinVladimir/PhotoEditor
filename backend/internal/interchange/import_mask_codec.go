package interchange

import (
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// The qualifier verifies installed model locks, the physical mask/warp arrays,
// source geometry and protected-mask semantics. JSON validity is insufficient.
type ImportMaskQualifier interface{QualifyImportedMask(context.Context,review.MaskRef,v2input.SessionVersionBinding)error}
type ImportMaskCodec struct{Qualifier ImportMaskQualifier;Warps ImportDenseWarpRemapper;Fallback ImportDependencyCodec}
func(c ImportMaskCodec)RemapImportDocument(ctx context.Context,contract string,raw []byte,old,target v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if contract!="MaskRef"{if c.Fallback!=nil{return c.Fallback.RemapImportDocument(ctx,contract,raw,old,target,resolve)};return nil,&ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 if c.Qualifier==nil||resolve==nil{return nil,v2input.ErrInvalid}
 mask,err:=review.ParseMaskRef(raw);if err!=nil{return nil,err}
 canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{return nil,err}
 if canvas!=old.CanvasHash{return nil,v2input.ErrInvalid}
 if canvas!=target.CanvasHash{return nil,&ImportRecipeUnsupported{Reason:"mask_canvas_remap_required"}}
 var fields map[string]json.RawMessage;if err:=json.Unmarshal(raw,&fields);err!=nil{return nil,err}
 mapped,err:=resolve(v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256},"MaskPixels");if err!=nil{return nil,err}
 if !validImportRef(mapped)||mapped.ArtifactID==mask.Artifact.ID{return nil,v2input.ErrInvalid}
 fields["artifact"],err=canonical.Marshal(mapped);if err!=nil{return nil,err}
 var chain struct{Steps []map[string]json.RawMessage `json:"steps"`;Hash string `json:"hash"`}
 if err:=json.Unmarshal(mask.TransformChain,&chain);err!=nil{return nil,err}
 for _,step:=range chain.Steps{if value,ok:=step["inverse_field"];ok{
  if err:=ctx.Err();err!=nil{return nil,err}
  var ref v2input.Ref;if err:=json.Unmarshal(value,&ref);err!=nil{return nil,err}
  if c.Warps==nil{return nil,&ImportRecipeUnsupported{Reason:"warp_occurrence_remapper_not_installed"}}
  var from,to review.Canvas
  if err:=json.Unmarshal(step["from_canvas"],&from);err!=nil{return nil,err}
  if err:=json.Unmarshal(step["to_canvas"],&to);err!=nil{return nil,err}
  mapped,err:=c.Warps.RemapImportedDenseWarp(ctx,ref,from,to,target);if err!=nil{return nil,err}
  if !validImportRef(mapped)||mapped.ArtifactID==ref.ArtifactID{return nil,v2input.ErrInvalid}
  step["inverse_field"],err=canonical.Marshal(mapped);if err!=nil{return nil,err}
 }}
 chain.Hash,err=canonical.Hash(struct{Steps []map[string]json.RawMessage `json:"steps"`}{chain.Steps});if err!=nil{return nil,err}
 fields["transform_chain"],err=canonical.Marshal(chain);if err!=nil{return nil,err}
 if mask.ObjectID!=nil{
  if *mask.ObjectID==uuid.Nil{return nil,v2input.ErrInvalid}
  // Portable object identity is scoped to the new asset; no person/consent
  // identity or approval is inherited from the archived UUID.
  id:=uuid.NewSHA1(target.AssetVersionID,[]byte("interchange:mask-object:"+mask.ObjectID.String()))
  fields["object_id"],err=canonical.Marshal(id);if err!=nil{return nil,err}
 }
 prepared,err:=canonical.Marshal(fields);if err!=nil{return nil,err}
 if err:=c.QualifyRemappedImportDocument(ctx,contract,prepared,target);err!=nil{return nil,err}
 return prepared,ctx.Err()
}
func(c ImportMaskCodec)QualifyRemappedImportDocument(ctx context.Context,contract string,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if contract!="MaskRef"{if c.Fallback!=nil{return c.Fallback.QualifyRemappedImportDocument(ctx,contract,raw,target)};return &ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 if c.Qualifier==nil||target.AssetVersionID==uuid.Nil||!validImportRef(target.Source){return v2input.ErrInvalid}
 mask,err:=review.ParseMaskRef(raw);if err!=nil{return err}
 canvas,err:=canonical.Hash(mask.Canvas);if err!=nil{return err};if canvas!=target.CanvasHash{return v2input.ErrInvalid}
 if err:=c.Qualifier.QualifyImportedMask(ctx,mask,target);err!=nil{return err}
 return ctx.Err()
}

func(c ImportMaskCodec)BindImportDocumentSource(source v2input.Ref)ImportDependencyCodec{
 if binder,ok:=c.Fallback.(ImportDocumentSourceBinder);ok{c.Fallback=binder.BindImportDocumentSource(source)}
 return c
}
