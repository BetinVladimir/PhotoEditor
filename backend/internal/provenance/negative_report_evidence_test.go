package provenance

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

func TestSigningNegativeReportBindsAdoptedRootAndParams(t *testing.T){
 for _,mode:=range []string{"valid","status","reason","source","identity","transform","outside","skipped","parent","not_review_required","changed_report"}{
  t.Run(mode,func(t *testing.T){
   pin:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
   params:=v2optical.NegativeParams{SchemaVersion:1,Base:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:pin(),CanvasHash:strings.Repeat("b",64)},Strength:1,FilmBaseRGB:[3]float64{.9,.8,.7},Calibration:pin()}
   paramsRaw,paramsSHA,err:=params.Compile();if err!=nil{t.Fatal(err)}
   paramsRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:paramsSHA}
   reason:="negative_review"
   report:=v2draft.ImageReportDocument{SchemaVersion:1,Status:"review",Operation:"optical.negative",Reason:&reason,Sources:[]v2input.SessionVersionBinding{params.Base},Registrations:[]v2input.Ref{}}
   switch mode{
   case "status":report.Status="pass"
   case "reason":reason="other"
   case "source":report.Sources[0].Source=pin()
   case "identity":r:=pin();report.Identity=&r
   case "transform":r:=pin();report.Transform=&r
   case "outside":report.OutsideMaskMaxError=.1
   case "skipped":report.SkippedCount=1
   case "parent":report.ParentReports=[]v2input.Ref{pin()}
   }
   reportRaw,reportRef:=contractBytes(t,report)
   resultRaw,resultRef:=contractBytes(t,v2draft.ImageResultDocument{SchemaVersion:1,WorkingImage:pin(),ChangedMask:pin(),Report:reportRef})
   outputs,_:=canonical.Marshal([]v2input.Ref{resultRef})
   task:=CanonicalSigningTask{Task:uuid.New(),Run:uuid.New(),Result:uuid.New(),Attempt:1,Fence:1,Operation:"optical.negative",Version:"1",Params:paramsRef,InputDigest:strings.Repeat("c",64),ResultDigest:strings.Repeat("d",64),Inputs:json.RawMessage(`[]`),Outputs:outputs,Metrics:json.RawMessage(`{}`),ReviewRequired:mode!="not_review_required"}
   who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};ready:=ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),ExportID:uuid.New()}
   adoption:=signingNegativeAdoptionFunc(func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask)(v2input.Ref,error){return resultRef,nil})
   reportReads:=0
   documents:=signingDocumentsFunc(func(_ context.Context,_ auth.Identity,_ ReadySigningExport,_ CanonicalSigningTask,r v2input.Ref,maximum int)([]byte,error){
    switch r{
    case resultRef:return resultRaw,nil
    case paramsRef:return paramsRaw,nil
    case reportRef:
     reportReads++;if mode=="changed_report"&&reportReads>1{return []byte(`{}`),nil};return reportRaw,nil
    default:t.Fatal("unexpected document",r);return nil,ErrInvalid
    }
   })
   actual,got,err:=ReadSigningNegativeReport(context.Background(),adoption,documents,who,ready,task)
   if mode=="valid"{if err!=nil||actual!=reportRef||got.Operation!="optical.negative"{t.Fatal("valid report refused",err)};return}
   if err==nil||actual.ArtifactID!=uuid.Nil{t.Fatal("invalid report accepted",mode)}
  })
 }
}
