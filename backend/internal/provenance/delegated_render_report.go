package provenance

import (
 "bytes"
 "context"
 "slices"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

func ReadDelegatedRenderReport(ctx context.Context,documents DelegatedTaskDocuments,
 task CanonicalSigningTask,binding v2input.SessionVersionBinding,
 reference v2input.Ref,parents []v2input.Ref)(v2draft.ImageReportDocument,error){
 var zero v2draft.ImageReportDocument
 if err:=ctx.Err();err!=nil{return zero,err}
 if documents==nil||binding.AssetVersionID==uuid.Nil||!ref(binding.Source)||
  !digest(binding.CanvasHash)||task.Operation!="render.compose"||task.Version!="1"||
  normalizeCanonicalSigningTask(&task)!=nil||!ref(reference)||
  parents==nil||len(parents)>33{return zero,ErrInvalid}
 seen:=make(map[uuid.UUID]bool,len(parents))
 for _,parent:=range parents{
  if !ref(parent)||parent.ArtifactID==reference.ArtifactID||
   seen[parent.ArtifactID]{return zero,ErrInvalid}
  seen[parent.ArtifactID]=true
 }
 expected:=append([]v2input.Ref{},parents...)
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 raw,err:=documents.ReadDelegatedTaskDocument(ctx,owned,reference,1<<20)
 if err!=nil{return zero,err}
 if !pinned(raw,reference,1<<20){return zero,ErrInvalid}
 body:=append([]byte(nil),raw...)
 report,err:=v2draft.ParseImageReport(body,reference)
 if err!=nil||report.Operation!="render.compose"||
  report.Status!="pass"&&report.Status!="review"||
  len(report.Sources)!=1||report.Sources[0]!=binding||
  len(report.Registrations)!=0||report.ParentReports==nil||
  !slices.Equal(report.ParentReports,expected){return zero,ErrInvalid}
 current,err:=documents.ReadDelegatedTaskDocument(ctx,owned,reference,1<<20)
 if err!=nil{return zero,err}
 if !bytes.Equal(current,body){return zero,ErrInvalid}
 return report,ctx.Err()
}
