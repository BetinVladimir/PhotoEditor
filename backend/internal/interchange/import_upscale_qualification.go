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
type BoundImportUpscaleOperation struct{Masters ImportMasterReader;Targets ImportBackgroundTargetAuthority;ModelLocks map[int]string;Policy ImportLayerOperationQualifier}
func(q BoundImportUpscaleOperation)QualifyImportedLayerOperation(ctx context.Context,operation,version string,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if operation!="image.upscale"||version!="1"||q.Masters==nil||q.Targets==nil||q.Policy==nil{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...);p,err:=decodeImportUpscaleParams(body);if err!=nil{return err}
 if p.Image!=binding.Source||p.Lock!=q.ModelLocks[p.Scale]||!validImportRefForSHA(q.ModelLocks[p.Scale]){return v2input.ErrInvalid}
 if err:=q.Targets.QualifyImportBackgroundTarget(ctx,binding);err!=nil{return err}
 master,err:=q.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};master=append([]byte(nil),master...)
 if len(master)<2||len(master)>1<<20||rawSHA(master)!=binding.Source.SHA256{return v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(master);if err!=nil{return err};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return err}
 if canvas!=binding.CanvasHash||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"{return v2input.ErrInvalid}
 if _,_,err:=importUpscaleOutputSize(image.Canvas.Width,image.Canvas.Height,p.Scale);err!=nil{return err}
 for round:=0;round<2;round++{
  if err:=ctx.Err();err!=nil{return err}
  if err:=q.Policy.QualifyImportedLayerOperation(ctx,operation,version,append([]byte(nil),body...),binding);err!=nil{return err}
  current,err:=q.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};if !bytes.Equal(current,master){return v2input.ErrInvalid}
  if err:=q.Targets.QualifyImportBackgroundTarget(ctx,binding);err!=nil{return err}
 }
 return ctx.Err()
}
func bindImportUpscaleOperation(ctx context.Context,d ImportRecipeCodecDependencies)(*ImportOperationRouter,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if d.Operations==nil{return nil,v2input.ErrInvalid}
 key:=ImportOperationVersion{"image.upscale","1"};policy,installed:=d.Operations.operations[key]
 if !installed{return d.Operations,nil}
 locks,err:=ownedImportUpscaleLocks(d.UpscaleModelLocks);if err!=nil{return nil,err}
 targets,err:=NewBoundImportBackgroundAuthority(d.Bindings,d.Sources);if err!=nil{return nil,err}
 operations:=make(map[ImportOperationVersion]ImportLayerOperationQualifier,len(d.Operations.operations))
 for k,v:=range d.Operations.operations{operations[k]=v}
 operations[key]=BoundImportUpscaleOperation{Masters:d.Masters,Targets:targets,ModelLocks:locks,Policy:policy}
 return NewImportOperationRouter(ctx,operations)
}

func ownedImportUpscaleLocks(configured map[int]string)(map[int]string,error){
 if len(configured)<1||len(configured)>2{return nil,v2input.ErrInvalid}
 owned:=map[int]string{}
 for scale,lock:=range configured{if (scale!=2&&scale!=4)||!validImportRefForSHA(lock){return nil,v2input.ErrInvalid};owned[scale]=lock}
 return owned,nil
}
func importUpscaleOutputSize(width,height int64,scale int)(int64,int64,error){
 if width<1||height<1||(scale!=2&&scale!=4){return 0,0,v2input.ErrInvalid}
 // Divide before multiplication, even for malformed oversized dimensions.
 maximumInputPixels:=int64(200000000)/(int64(scale)*int64(scale))
 if width>maximumInputPixels/height{return 0,0,v2input.ErrInvalid}
 return width*int64(scale),height*int64(scale),nil
}
