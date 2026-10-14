package interchange
import (
 "bytes"
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Full runtime/device/model qualification is still mandatory in Policy. This
// boundary independently binds Params to the current canonical ingest source.
type BoundImportDenoiseOperation struct{Masters ImportMasterReader;Targets ImportBackgroundTargetAuthority;ModelLock string;Policy ImportLayerOperationQualifier}
func(q BoundImportDenoiseOperation)QualifyImportedLayerOperation(ctx context.Context,operation,version string,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if operation!="image.denoise"||version!="1"||q.Masters==nil||q.Targets==nil||q.Policy==nil||!validImportRefForSHA(q.ModelLock){return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...);p,err:=decodeImportDenoiseParams(body);if err!=nil{return err}
 if p.Image!=binding.Source||p.Lock!=q.ModelLock{return v2input.ErrInvalid}
 if err:=q.Targets.QualifyImportBackgroundTarget(ctx,binding);err!=nil{return err}
 master,err:=q.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};master=append([]byte(nil),master...)
 if len(master)<2||len(master)>1<<20||rawSHA(master)!=binding.Source.SHA256{return v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(master);if err!=nil{return err};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return err}
 if canvas!=binding.CanvasHash||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"{return v2input.ErrInvalid}
 for round:=0;round<2;round++{
  if err:=ctx.Err();err!=nil{return err}
  if err:=q.Policy.QualifyImportedLayerOperation(ctx,operation,version,append([]byte(nil),body...),binding);err!=nil{return err}
  current,err:=q.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};if !bytes.Equal(current,master){return v2input.ErrInvalid}
  if err:=q.Targets.QualifyImportBackgroundTarget(ctx,binding);err!=nil{return err}
 }
 return ctx.Err()
}
func bindImportDenoiseOperation(ctx context.Context,d ImportRecipeCodecDependencies)(*ImportOperationRouter,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if d.Operations==nil{return nil,v2input.ErrInvalid}
 key:=ImportOperationVersion{"image.denoise","1"};policy,installed:=d.Operations.operations[key]
 if !installed{return d.Operations,nil}
 if !validImportRefForSHA(d.DenoiseModelLock){return nil,v2input.ErrInvalid}
 targets,err:=NewBoundImportBackgroundAuthority(d.Bindings,d.Sources);if err!=nil{return nil,err}
 operations:=make(map[ImportOperationVersion]ImportLayerOperationQualifier,len(d.Operations.operations))
 for k,v:=range d.Operations.operations{operations[k]=v}
 operations[key]=BoundImportDenoiseOperation{Masters:d.Masters,Targets:targets,ModelLock:d.DenoiseModelLock,Policy:policy}
 return NewImportOperationRouter(ctx,operations)
}
