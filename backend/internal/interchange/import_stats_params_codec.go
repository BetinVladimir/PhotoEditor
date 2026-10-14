package interchange

import (
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type importStatsParams struct{Image v2input.Ref `json:"image"`;Person json.RawMessage `json:"person"`}
func isImportStatsParams(kind string,raw []byte)bool{
 if kind!="Params"{return false};var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return false}
 // This pair is the exact StatsParams shape. Other operation shapes keep their
 // own dispatch; malformed Stats-shaped documents are rejected by decoding.
 _,image:=fields["image"];_,person:=fields["person"]
 return image&&person&&len(fields)==2
}
func decodeImportStatsParams(raw []byte)(importStatsParams,error){
 var p importStatsParams
 if err:=decodeImportDocument(raw,&p);err!=nil{return p,err}
 if !validImportRef(p.Image){return p,v2input.ErrInvalid}
 mask,err:=review.ParseMaskRef(p.Person);if err!=nil{return p,err}
 if mask.Selector!="person"{return p,v2input.ErrInvalid}
 return p,nil
}
// StatsParams has an inline person mask, not a reference to a MaskRef document.
// Physical pixels and any inverse fields are remapped by the shared mask codec.
type ImportStatsParamsCodec struct{Qualifier ImportLayerOperationQualifier;Masks ImportMaskCodec;Fallback ImportDependencyCodec}
func(c ImportStatsParamsCodec)RemapImportDocument(ctx context.Context,kind string,raw []byte,old,target v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !isImportStatsParams(kind,raw){if c.Fallback!=nil{return c.Fallback.RemapImportDocument(ctx,kind,raw,old,target,resolve)};return nil,&ImportRecipeUnsupported{Reason:"params_contract_not_installed"}}
 p,err:=decodeImportStatsParams(append([]byte(nil),raw...));if err!=nil{return nil,err}
 if c.Qualifier==nil||resolve==nil||old.AssetVersionID==uuid.Nil||target.AssetVersionID==uuid.Nil||!validImportRefForSHA(old.CanvasHash)||!validImportRefForSHA(target.CanvasHash)||p.Image!=old.Source||!validImportRef(target.Source)||old.AssetVersionID==target.AssetVersionID||old.Source.ArtifactID==target.Source.ArtifactID{return nil,v2input.ErrInvalid}
 if old.CanvasHash!=target.CanvasHash{return nil,&ImportRecipeUnsupported{Reason:"params_canvas_remap_required"}}
 person,err:=c.Masks.RemapImportDocument(ctx,"MaskRef",p.Person,old,target,resolve);if err!=nil{return nil,err}
 p.Image,p.Person=target.Source,person
 prepared,err:=canonical.Marshal(p);if err!=nil{return nil,err}
 if err:=c.QualifyRemappedImportDocument(ctx,kind,prepared,target);err!=nil{return nil,err}
 return prepared,ctx.Err()
}
func(c ImportStatsParamsCodec)QualifyRemappedImportDocument(ctx context.Context,kind string,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if !isImportStatsParams(kind,raw){if c.Fallback!=nil{return c.Fallback.QualifyRemappedImportDocument(ctx,kind,raw,target)};return &ImportRecipeUnsupported{Reason:"params_contract_not_installed"}}
 p,err:=decodeImportStatsParams(raw);if err!=nil{return err}
 if c.Qualifier==nil||target.AssetVersionID==uuid.Nil||!validImportRefForSHA(target.CanvasHash)||p.Image!=target.Source{return v2input.ErrInvalid}
 if err:=c.Masks.QualifyRemappedImportDocument(ctx,"MaskRef",p.Person,target);err!=nil{return err}
 if err:=c.Qualifier.QualifyImportedLayerOperation(ctx,"color.stats","1",append([]byte(nil),raw...),target);err!=nil{return err}
 return ctx.Err()
}
func(c ImportStatsParamsCodec)BindImportDocumentSource(source v2input.Ref)ImportDependencyCodec{
 if binder,ok:=c.Fallback.(ImportDocumentSourceBinder);ok{c.Fallback=binder.BindImportDocumentSource(source)};return c
}
