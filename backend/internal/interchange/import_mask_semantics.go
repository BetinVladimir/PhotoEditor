package interchange
import (
 "bytes"
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Geometry/source authority is enforced here independently of the operator
// policy. Policy must still prove current model/runtime/consent admission.
type BoundImportMaskSemantics struct{
 Masters ImportMasterReader
 Targets ImportBackgroundTargetAuthority
 Policy ImportMaskQualifier
}
func(q BoundImportMaskSemantics)QualifyImportedMask(ctx context.Context,mask review.MaskRef,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if q.Masters==nil||q.Targets==nil||q.Policy==nil||!validImportRef(binding.Source){return v2input.ErrInvalid}
 raw,err:=canonical.Marshal(mask);if err!=nil{return err}
 owned,err:=review.ParseMaskRef(raw);if err!=nil{return err}
 if err:=q.Targets.QualifyImportBackgroundTarget(ctx,binding);err!=nil{return err}
 master,err:=q.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};master=append([]byte(nil),master...)
 if len(master)<2||len(master)>1<<20||rawSHA(master)!=binding.Source.SHA256{return v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(master);if err!=nil{return err}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return err}
 maskChain,err:=canonical.Bytes(owned.TransformChain);if err!=nil{return err}
 imageChain,err:=canonical.Bytes(image.TransformChain);if err!=nil{return err}
 if canvas!=binding.CanvasHash||owned.Canvas!=image.Canvas||!bytes.Equal(maskChain,imageChain)||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"{return v2input.ErrInvalid}
 // Repeat policy after source/target IO. Each round gets a fresh parsed
 // mask so callback mutations cannot change subsequent authorization inputs.
 for round:=0;round<2;round++{
  if err:=ctx.Err();err!=nil{return err}
  policyMask,err:=review.ParseMaskRef(append([]byte(nil),raw...));if err!=nil{return err}
  if err:=q.Policy.QualifyImportedMask(ctx,policyMask,binding);err!=nil{return err}
  if err:=ctx.Err();err!=nil{return err}
  current,err:=q.Masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err}
  if !bytes.Equal(master,current){return v2input.ErrInvalid}
  if err:=q.Targets.QualifyImportBackgroundTarget(ctx,binding);err!=nil{return err}
 }

 return ctx.Err()
}
