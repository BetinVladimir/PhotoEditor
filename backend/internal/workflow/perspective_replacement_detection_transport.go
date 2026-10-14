package workflow

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// BuildInputClosure binds transport bytes to the freshly compiled replacement
// packet before handing them to a dedicated publication repository. The caller
// must retain physical source authority in that repository; this is not admission.
func (p PerspectiveReplacementDetectionPlan) BuildInputClosure(ctx context.Context, params review.ArtifactRef, out v2input.Publisher) (PerspectiveLinesClosure, error) {
 var zero PerspectiveLinesClosure
 _, _, task, _, err := p.PreparePublication(ctx)
 if err != nil { return zero, err }
 if params != task.Params || out == nil { return zero, v2input.ErrInvalid }
 publisher := &replacementDetectionTransportPublisher{plan:p.detector, params:params, out:out}
 return BuildPerspectiveLinesClosure(ctx,p.detector,params,publisher)
}

type replacementDetectionTransportPublisher struct {
 plan PerspectiveLinesPlan
 params review.ArtifactRef
 out v2input.Publisher
 chunk *v2input.Ref
 finished bool
}

func (p *replacementDetectionTransportPublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, raw []byte) (v2input.Ref,error) {
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 if task!=p.plan.task || p.finished || (ordinal==0 && p.chunk!=nil) || (ordinal==1 && p.chunk==nil) {return v2input.Ref{},v2input.ErrInvalid}
 document,err:=CompilePerspectiveLinesTransportDocument(p.plan,p.params,ordinal,contract,raw,p.chunk)
 if err!=nil{return v2input.Ref{},err}
 ref,err:=p.out.Publish(ctx,task,ordinal,contract,document.Bytes())
 if err!=nil{return v2input.Ref{},err}
 if ref.ArtifactID==uuid.Nil || ref.SHA256!=document.SHA256(){return v2input.Ref{},v2input.ErrInvalid}
 entries,err:=p.plan.Entries(p.params);if err!=nil{return v2input.Ref{},err}
 for _,entry:=range entries{if ref.ArtifactID==entry.Ref.ArtifactID{return v2input.Ref{},v2input.ErrInvalid}}
 if ordinal==0{owned:=ref;p.chunk=&owned}else{
  if ref.ArtifactID==p.chunk.ArtifactID{return v2input.Ref{},v2input.ErrInvalid}
  p.finished=true
 }
 return ref,nil
}
