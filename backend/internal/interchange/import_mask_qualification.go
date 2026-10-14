package interchange

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Catalog must authorize the remapped ready dependency in the current import
// workflow/source, not merely find an artifact UUID anywhere in the tenant.
type ImportPhysicalMaskQualifier struct{
 masters ImportMasterReader
 catalog ImportDocumentCatalog
 objects ImportDocumentObjects
 locks map[string]bool
 maxBytes int64
}
func NewImportPhysicalMaskQualifier(masters ImportMasterReader,catalog ImportDocumentCatalog,objects ImportDocumentObjects,allowedModelLocks []string,maxBytes int64)(*ImportPhysicalMaskQualifier,error){
 if masters==nil||catalog==nil||objects==nil||maxBytes<1||maxBytes>128<<30||len(allowedModelLocks)>1024{return nil,v2input.ErrInvalid}
 locks:=map[string]bool{}
 for _,lock:=range allowedModelLocks{if !validImportRef(v2input.Ref{ArtifactID:uuid.NameSpaceOID,SHA256:lock})||locks[lock]{return nil,v2input.ErrInvalid};locks[lock]=true}
 return &ImportPhysicalMaskQualifier{masters:masters,catalog:catalog,objects:objects,locks:locks,maxBytes:maxBytes},nil
}
func(q *ImportPhysicalMaskQualifier)QualifyImportedMask(ctx context.Context,mask review.MaskRef,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if q==nil||q.masters==nil||q.catalog==nil||q.objects==nil||q.maxBytes<1||binding.AssetVersionID==uuid.Nil||!validImportRef(binding.Source){return v2input.ErrInvalid}
 raw,err:=canonical.Marshal(mask);if err!=nil{return err};if _,err:=review.ParseMaskRef(raw);err!=nil{return err}
 if mask.ModelLockID==nil{if mask.Selector!="manual"{return &ImportRecipeUnsupported{Reason:"mask_model_lock_required"}}}else if !q.locks[*mask.ModelLockID]{return &ImportRecipeUnsupported{Reason:"mask_model_lock_not_installed"}}
 master,err:=q.masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};master=append([]byte(nil),master...)
 if len(master)<2||len(master)>1<<20||rawSHA(master)!=binding.Source.SHA256{return v2input.ErrInvalid}
 var typed review.WorkingImage;if err:=decodeImportDocument(master,&typed);err!=nil{return err}
 image,err:=review.ParseWorkingImage(master);if err!=nil{return err}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return err}
 if canvas!=binding.CanvasHash||mask.Canvas!=image.Canvas||image.DType!="float32"||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"{return v2input.ErrInvalid}
 var consumed int64
 verify:=func(ref v2input.Ref,kind string,width,height int64,channels int,unit bool)error{
  if err:=ctx.Err();err!=nil{return err}
  if !validImportRef(ref){return v2input.ErrInvalid}
  node:=v2input.Entry{Ref:ref,Contract:kind}
  object,err:=q.catalog.LookupImportDocument(ctx,node);if err!=nil{return err}
  if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.SHA256!=ref.SHA256||object.Bytes<1||object.Bytes>q.maxBytes-consumed||(object.ContentType!="application/octet-stream"&&object.ContentType!="application/x-npy"){return v2input.ErrInvalid}
  reader,err:=q.objects.Open(ctx,object);if err!=nil{if reader!=nil{_ = reader.Close()};return err};if reader==nil{return v2input.ErrInvalid}
  verifyErr:=masknpy.VerifyFloat32ArrayNPY(ctx,reader,object.Bytes,object.SHA256,width,height,channels,unit);closeErr:=reader.Close()
  if verifyErr!=nil{return verifyErr};if closeErr!=nil{return closeErr}
  if err:=ctx.Err();err!=nil{return err}
  current,err:=q.catalog.LookupImportDocument(ctx,node);if err!=nil{return err};if current!=object{return v2input.ErrInvalid}
  consumed+=object.Bytes;return nil
 }
 if err:=verify(v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256},"MaskPixels",mask.Canvas.Width,mask.Canvas.Height,0,true);err!=nil{return err}
 var chain struct{Steps []struct{To review.Canvas `json:"to_canvas"`;Inverse *v2input.Ref `json:"inverse_field"`} `json:"steps"`}
 if err:=json.Unmarshal(mask.TransformChain,&chain);err!=nil{return err}
 for _,step:=range chain.Steps{if step.Inverse!=nil{if err:=verify(*step.Inverse,"DenseWarpField",step.To.Width,step.To.Height,2,false);err!=nil{return err}}}
 current,err:=q.masters.ReadImportMaster(ctx,binding.Source,1<<20);if err!=nil{return err};if !bytes.Equal(current,master){return v2input.ErrInvalid}
 return ctx.Err()
}
