package provenance

import (
 "bytes"
 "context"

 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

// Report status records algorithm review requirements, never human acceptance.
// Read the report only through the adopted root, not a caller-selected child.
func ReadSigningNegativeReport(ctx context.Context, adoption SigningNegativeAdoptionReader, documents SigningTaskDocumentReader, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask)(v2input.Ref,v2draft.ImageReportDocument,error){
 var zero v2draft.ImageReportDocument
 root,result,err:=ReadSigningNegativeResult(ctx,adoption,documents,who,ready,task)
 if err!=nil{return v2input.Ref{},zero,err}
 paramsRaw,err:=documents.ReadSigningTaskDocument(ctx,who,ready,task,task.Params,65536)
 if err!=nil{return v2input.Ref{},zero,err}
 paramsRaw=append([]byte(nil),paramsRaw...)
 if !pinned(paramsRaw,task.Params,65536){return v2input.Ref{},zero,ErrInvalid}
 params,err:=v2optical.ParseNegativeParams(paramsRaw)
 if err!=nil{return v2input.Ref{},zero,ErrInvalid}
 raw,err:=documents.ReadSigningTaskDocument(ctx,who,ready,task,result.Report,1<<20)
 if err!=nil{return v2input.Ref{},zero,err}
 raw=append([]byte(nil),raw...)
 if !pinned(raw,result.Report,1<<20){return v2input.Ref{},zero,ErrInvalid}
 report,err:=v2draft.ParseImageReport(raw,result.Report)
 if err!=nil||report.Operation!="optical.negative"||report.Status!="review"||report.Reason==nil||*report.Reason!="negative_review"||report.SkippedCount!=0||report.OutsideMaskMaxError!=0||report.Identity!=nil||report.Transform!=nil||len(report.Sources)!=1||report.Sources[0]!=params.Base||len(report.Registrations)!=0||len(report.ParentReports)!=0||!task.ReviewRequired{return v2input.Ref{},zero,ErrInvalid}
 for _,document:=range []struct{pin v2input.Ref;raw []byte;maximum int}{{task.Params,paramsRaw,65536},{result.Report,raw,1<<20}}{
  current,err:=documents.ReadSigningTaskDocument(ctx,who,ready,task,document.pin,document.maximum)
  if err!=nil{return v2input.Ref{},zero,err};if !bytes.Equal(current,document.raw){return v2input.Ref{},zero,ErrInvalid}
 }
 currentRoot,currentResult,err:=ReadSigningNegativeResult(ctx,adoption,documents,who,ready,task)
 if err!=nil{return v2input.Ref{},zero,err}
 if currentRoot!=root||currentResult.WorkingImage!=result.WorkingImage||currentResult.ChangedMask!=result.ChangedMask||currentResult.Report!=result.Report||currentResult.GeneratedMask!=nil{return v2input.Ref{},zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return v2input.Ref{},zero,err}
 return result.Report,report,nil
}
