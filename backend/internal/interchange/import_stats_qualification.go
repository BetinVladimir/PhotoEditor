package interchange
import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Stats algorithm qualification and person-model qualification are independent.
// Neither a structurally valid Params document nor a previous report proves
// current source/consent/physical mask authority.
type BoundImportStatsOperation struct{Masters ImportMasterReader;Targets ImportBackgroundTargetAuthority;Masks ImportMaskQualifier;Policy ImportLayerOperationQualifier}
func(q BoundImportStatsOperation)QualifyImportedLayerOperation(ctx context.Context,operation,version string,raw []byte,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if operation!="color.stats"||version!="1"||q.Masters==nil||q.Targets==nil||q.Masks==nil||q.Policy==nil||binding.AssetVersionID==uuid.Nil||!validImportRef(binding.Source)||!validImportRefForSHA(binding.CanvasHash){return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...);p,err:=decodeImportStatsParams(body);if err!=nil{return err};if p.Image!=binding.Source{return v2input.ErrInvalid}
 if err:=q.Targets.QualifyImportBackgroundTarget(ctx,binding);err!=nil{return err}
 master,err:=q.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};master=append([]byte(nil),master...)
 if len(master)<2||len(master)>1<<20||rawSHA(master)!=binding.Source.SHA256{return v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(master);if err!=nil{return err};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return err}
 mask,err:=review.ParseMaskRef(p.Person);if err!=nil{return err};maskChain,err:=canonical.Bytes(mask.TransformChain);if err!=nil{return err};imageChain,err:=canonical.Bytes(image.TransformChain);if err!=nil{return err}
 if canvas!=binding.CanvasHash||mask.Canvas!=image.Canvas||!bytes.Equal(maskChain,imageChain)||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"{return v2input.ErrInvalid}
 for round:=0;round<2;round++{
  if err:=ctx.Err();err!=nil{return err}
  ownedMask,err:=review.ParseMaskRef(append([]byte(nil),p.Person...));if err!=nil{return err}
  if err:=q.Masks.QualifyImportedMask(ctx,ownedMask,binding);err!=nil{return err}
  if err:=ctx.Err();err!=nil{return err}
  if err:=q.Policy.QualifyImportedLayerOperation(ctx,operation,version,append([]byte(nil),body...),binding);err!=nil{return err}
  current,err:=q.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};if !bytes.Equal(current,master){return v2input.ErrInvalid}
  if err:=q.Targets.QualifyImportBackgroundTarget(ctx,binding);err!=nil{return err}
 }
 return ctx.Err()
}
func bindImportStatsOperation(ctx context.Context,d ImportRecipeCodecDependencies)(*ImportOperationRouter,error){
 if err:=ctx.Err();err!=nil{return nil,err};if d.Operations==nil{return nil,v2input.ErrInvalid}
 key:=ImportOperationVersion{"color.stats","1"};policy,installed:=d.Operations.operations[key];if !installed{return d.Operations,nil}
 if d.Masters==nil||d.Masks==nil{return nil,v2input.ErrInvalid}
 targets,err:=NewBoundImportBackgroundAuthority(d.Bindings,d.Sources);if err!=nil{return nil,err}
 operations:=make(map[ImportOperationVersion]ImportLayerOperationQualifier,len(d.Operations.operations));for k,v:=range d.Operations.operations{operations[k]=v}
 operations[key]=BoundImportStatsOperation{Masters:d.Masters,Targets:targets,Masks:d.Masks,Policy:policy}
 return NewImportOperationRouter(ctx,operations)
}
