package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
type exportProposalInputsStub struct{ref v2input.Ref;body []byte;frame ExportFrame;err error;calls int;mutate,missingFrames bool}
func(s *exportProposalInputsStub)ResolveExportProposalInputs(_ context.Context,_ auth.Identity,_ uuid.UUID,request CreateRequest,_ string)(v2input.Ref,[]byte,ExportFrameResolver,error){
 s.calls++;if s.mutate{*request.SnapshotID=uuid.New()}
 var resolver ExportFrameResolver=exportFrameResolverFunc(func(context.Context,v2input.SessionSnapshotItem)(ExportFrame,error){return s.frame,nil});if s.missingFrames{resolver=nil}
 return s.ref,s.body,resolver,s.err
}
func TestExportProposalSnapshotBindingAndOwnedIntent(t *testing.T){
 for _,mode:=range []string{"valid","mutation","hash","foreign_snapshot","missing_frames","source_error","limits","cancelled"}{t.Run(mode,func(t *testing.T){
  _,project,ref,body,frame:=exportPlanFixture(t);var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument(body,&snapshot);err!=nil{t.Fatal(err)}
  sid:=snapshot.SnapshotID;request:=CreateRequest{ProjectID:project,Direction:"export",Format:"photoeditor_bundle",SnapshotID:&sid};tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
  cause:=errors.New("snapshot unavailable");source:=&exportProposalInputsStub{ref:ref,body:body,frame:frame};p:=ExportProposalSource{Inputs:source,Limits:ExportLimits{ArchiveBytes:1<<20,ExpandedBytes:1<<20,Files:10}};ctx:=context.Background()
  switch mode{case "mutation":source.mutate=true;case "hash":source.ref.SHA256=rawSHA([]byte("substitution"));case "foreign_snapshot":sid=uuid.New();case "missing_frames":source.missingFrames=true;case "source_error":source.err=cause;case "limits":p.Limits.Files=0;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  frozen,err:=p.ProposeExportDocuments(ctx,who,tid,request,"proposal-key")
  if mode=="valid"||mode=="mutation"{
   if err!=nil||len(frozen.Body())==0{t.Fatal("valid proposal",err)}
   intent,err:=FreezeCreateIntent(tid,who,request,"proposal-key");if err!=nil{t.Fatal(err)}
   if _,_,err:=DecodeFrozenExportDocuments(context.Background(),intent,frozen.Body());err!=nil{t.Fatal("proposal scope",err)}
   old:=frozen.Body();source.body[0]='!';if string(frozen.Body())!=string(old){t.Fatal("source buffer alias")}
  }else if err==nil||len(frozen.Body())!=0{t.Fatal("invalid proposal exposed docs",err)}
  if mode=="source_error"&&!errors.Is(err,cause){t.Fatal("source error lost")}
  if (mode=="limits"||mode=="cancelled")&&source.calls!=0{t.Fatal("invalid preflight read source")}
 })}
}
