package v2optical
import (
 "context"
 "bytes"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
// ReplacementPerspectiveDetectionSource is source preflight only. It grants no
// publication/adoption authority and contains no old or synthetic line result.
type ReplacementPerspectiveDetectionSource struct {
 Tenant,Project,Review uuid.UUID
 DecisionHash string
 Principal auth.Identity
 Base v2input.SessionVersionBinding
 WorkingImage []byte
}
func(p PreparedMappedReplacementStep) PerspectiveDetectionSource(ctx context.Context)(ReplacementPerspectiveDetectionSource,error){
 var zero ReplacementPerspectiveDetectionSource
 if err:=ctx.Err();err!=nil{return zero,err}
 if p.parents.step.Operation()!="optical.perspective"{return zero,v2input.ErrInvalid}
 frozen,err:=ParsePerspectiveParams([]byte(p.parents.step.intent.raw));if err!=nil{return zero,err}
 if frozen.Mode!="auto"{return zero,v2input.ErrInvalid}
 bindings,err:=p.parents.mappedReplacementBindings(ctx,p.mapping);if err!=nil{return zero,err}
 base:=frozen.Base
 for _,binding:=range bindings{
  if binding.Before!=frozen.Base{return zero,v2input.ErrInvalid}
  base=binding.After
 }
 raw,err:=p.sourceDocument(ctx,base.Source);if err!=nil{return zero,err}
 image,err:=VerifyPerspectiveWorkingImage(perspectiveIdentityParams(base),raw);if err!=nil{return zero,err}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil||canvas!=base.CanvasHash{return zero,v2input.ErrInvalid}
 l:=p.parents.step.intent.locator
 node:=p.Node()
 if node.OriginalTask!=l.Task||node.OriginalRun!=l.Run||node.Run==uuid.Nil||node.Task==uuid.Nil{return zero,v2input.ErrInvalid}
 return ReplacementPerspectiveDetectionSource{Tenant:l.Tenant,Project:l.Project,Review:l.Review,DecisionHash:l.Hash,Principal:p.parents.step.intent.who,Base:base,WorkingImage:bytes.Clone(raw)},ctx.Err()
}
