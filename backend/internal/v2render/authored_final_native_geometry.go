package v2render

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "crypto/sha256"
 "encoding/hex"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// authoredNativeSingleGeometry validates content selected by the current scoped
// transcript. It is not a replacement for canonical producer/physical replay
// authority. In particular, no blend target participates in these checks.
func authoredNativeSingleGeometry(phase AuthoredPhaseReceipt,bodies map[v2input.Ref][]byte)(v2draft.DocumentGeometry,error){
 empty:=v2draft.DocumentGeometry{}
 if phase.Geometry==nil{return empty,v2input.ErrInvalid}
 native:=phase.Geometry
 read:=func(ref v2input.Ref)([]byte,error){
  raw,ok:=bodies[ref];if !ok{return nil,v2input.ErrInvalid}
  sum:=sha256.Sum256(raw);wire,err:=canonical.Bytes(raw)
  if err!=nil||!bytes.Equal(wire,raw)||hex.EncodeToString(sum[:])!=ref.SHA256{return nil,v2input.ErrInvalid}
  return raw,nil
 }
 root,err:=read(native.Root);if err!=nil{return empty,err}
 result,err:=v2draft.ParseImageResult(root,native.Root)
 if err!=nil||result.WorkingImage!=native.Image||result.Report!=native.Report{return empty,v2input.ErrInvalid}
 sourceRaw,err:=read(phase.InputSource.Source);if err!=nil{return empty,err}
 documents:=make(map[string][]byte,len(bodies));seen:=map[string]v2input.Ref{}
 for ref:=range bodies{
  if prior,ok:=seen[ref.ArtifactID.String()];ok&&prior!=ref{return empty,v2input.ErrInvalid}
  seen[ref.ArtifactID.String()]=ref
  // Every supplied auxiliary document must retain its exact physical digest.
  raw,e:=read(ref);if e!=nil{return empty,e};documents[ref.ArtifactID.String()]=raw
 }
 switch native.Task.Operation{
 case "optical.lens":
  err=verifyLensResultGeometry(AdoptedLensBinding{ResultID:native.Task.ResultID,Result:native.Root,Base:phase.InputSource},result,sourceRaw,documents)
 case "optical.perspective":
  err=verifyPerspectiveResultGeometry(AdoptedPerspectiveBinding{ResultID:native.Task.ResultID,Result:native.Root,Base:phase.InputSource},result,sourceRaw,documents)
 default:return empty,v2input.ErrInvalid
 }
 if err!=nil{return empty,err}
 image,err:=review.ParseWorkingImage(documents[native.Image.ArtifactID.String()]);if err!=nil{return empty,err}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return empty,err}
 chain,err:=canonical.Hash(image.TransformChain);if err!=nil{return empty,err}
 return v2draft.DocumentGeometry{CanvasHash:canvas,TransformHash:chain},nil
}

// nativeSingleGeometry obtains each JSON document from the same protected
// parent/instruction object scope. The descriptor/root cannot nominate arbitrary
// readable documents. Current transcript checks bracket this in Geometry.
func(s SQLAuthoredFinalCatalog)nativeGeometry(ctx context.Context,who auth.Identity,tenant uuid.UUID,phase AuthoredPhaseReceipt)(v2draft.DocumentGeometry,error){
 empty:=v2draft.DocumentGeometry{}
 if phase.Geometry==nil{return empty,v2input.ErrInvalid}
 switch phase.Geometry.Task.Operation{
 case "optical.panorama","optical.focus_stack":return s.nativeMultiGeometry(ctx,who,tenant,phase)
 case "image.upscale":return s.nativeUpscaleGeometry(ctx,who,tenant,phase)
 case "optical.lens","optical.perspective":default:return empty,v2input.ErrInvalid
 }
 bodies:=map[v2input.Ref][]byte{}
 read:=func(ref v2input.Ref)([]byte,error){
  if raw,ok:=bodies[ref];ok{return raw,nil}
  raw,err:=s.Objects.ReadJSON(ctx,who,tenant,s.Project,s.Parent,s.Ordinal,ref)
  if err!=nil{return nil,err};bodies[ref]=raw;return raw,nil
 }
 root,err:=read(phase.Geometry.Root);if err!=nil{return empty,err}
 parsed,err:=v2draft.ParseImageResult(root,phase.Geometry.Root)
 if err!=nil||parsed.WorkingImage!=phase.Geometry.Image||parsed.Report!=phase.Geometry.Report{return empty,v2input.ErrInvalid}
 for _,ref:=range []v2input.Ref{phase.InputSource.Source,parsed.WorkingImage,parsed.ChangedMask,parsed.Report}{if _,err=read(ref);err!=nil{return empty,err}}
 // Only the typed report's specific perspective transform edge is consumed;
 // no generic JSON traversal or arbitrary embedded reference is authority.
 var report struct{Transform *v2input.Ref `json:"transform"`}
 if json.Unmarshal(bodies[parsed.Report],&report)!=nil{return empty,v2input.ErrInvalid}
 if report.Transform!=nil{if _,err=read(*report.Transform);err!=nil{return empty,err}}
 return authoredNativeSingleGeometry(phase,bodies)
}
