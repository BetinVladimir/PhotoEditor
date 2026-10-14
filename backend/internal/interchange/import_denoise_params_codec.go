package interchange
import (
 "context"
 "encoding/json"
 "math"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type importDenoiseParams struct{
 Image v2input.Ref `json:"image"`
 Strength float64 `json:"strength"`
 Model string `json:"model_id"`
 Lock string `json:"model_lock_id"`
 Adapter string `json:"color_adapter_id"`
 Tile int `json:"tile"`
 Overlap int `json:"overlap"`
}
func decodeImportDenoiseParams(raw []byte)(importDenoiseParams,error){
 var p importDenoiseParams
 if err:=decodeImportDocument(raw,&p);err!=nil{return p,err}
 if !validImportRef(p.Image)||math.IsNaN(p.Strength)||math.IsInf(p.Strength,0)||p.Strength<0||p.Strength>1||p.Model!="scunet_color_real_psnr"||!validImportRefForSHA(p.Lock)||p.Adapter!="work_srgb_delta_v1"||(p.Tile!=256&&p.Tile!=512)||p.Overlap!=64{return p,v2input.ErrInvalid}
 return p,nil
}
type ImportDenoiseParamsCodec struct{Qualifier ImportLayerOperationQualifier;Fallback ImportDependencyCodec}
func isImportDenoiseParams(kind string,raw []byte)bool{
 if kind!="Params"{return false};var fields struct{Model string `json:"model_id"`};return json.Unmarshal(raw,&fields)==nil&&fields.Model=="scunet_color_real_psnr"
}
func(c ImportDenoiseParamsCodec)RemapImportDocument(ctx context.Context,kind string,raw []byte,old,target v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !isImportDenoiseParams(kind,raw){if c.Fallback!=nil{return c.Fallback.RemapImportDocument(ctx,kind,raw,old,target,resolve)};return nil,&ImportRecipeUnsupported{Reason:"params_contract_not_installed"}}
 p,err:=decodeImportDenoiseParams(append([]byte(nil),raw...));if err!=nil{return nil,err}
 if old.AssetVersionID==uuid.Nil||target.AssetVersionID==uuid.Nil||!validImportRefForSHA(old.CanvasHash)||!validImportRefForSHA(target.CanvasHash)||c.Qualifier==nil||p.Image!=old.Source||!validImportRef(target.Source)||old.AssetVersionID==target.AssetVersionID||old.Source.ArtifactID==target.Source.ArtifactID{return nil,v2input.ErrInvalid}
 if old.CanvasHash!=target.CanvasHash{return nil,&ImportRecipeUnsupported{Reason:"params_canvas_remap_required"}}
 // Source is the already qualified canonical ingest mapping. No archive
 // WorkingImage publication or caller-selected substitute is needed here.
 p.Image=target.Source
 prepared,err:=canonical.Marshal(p);if err!=nil{return nil,err}
 if err:=c.QualifyRemappedImportDocument(ctx,kind,prepared,target);err!=nil{return nil,err}
 return prepared,ctx.Err()
}
func(c ImportDenoiseParamsCodec)QualifyRemappedImportDocument(ctx context.Context,kind string,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if !isImportDenoiseParams(kind,raw){if c.Fallback!=nil{return c.Fallback.QualifyRemappedImportDocument(ctx,kind,raw,target)};return &ImportRecipeUnsupported{Reason:"params_contract_not_installed"}}
 p,err:=decodeImportDenoiseParams(raw);if err!=nil{return err}
 if target.AssetVersionID==uuid.Nil||!validImportRefForSHA(target.CanvasHash)||c.Qualifier==nil||p.Image!=target.Source{return v2input.ErrInvalid}
 if err:=c.Qualifier.QualifyImportedLayerOperation(ctx,"image.denoise","1",append([]byte(nil),raw...),target);err!=nil{return err};return ctx.Err()
}
func(c ImportDenoiseParamsCodec)BindImportDocumentSource(source v2input.Ref)ImportDependencyCodec{
 if binder,ok:=c.Fallback.(ImportDocumentSourceBinder);ok{c.Fallback=binder.BindImportDocumentSource(source)};return c
}
