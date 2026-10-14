package interchange

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type importMasterReaderStub struct{body []byte;calls int;cancel context.CancelFunc}
func(s *importMasterReaderStub)ReadImportMaster(_ context.Context,_ v2input.Ref,limit int64)([]byte,error){s.calls++;if limit!=1<<20{return nil,v2input.ErrInvalid};if s.cancel!=nil{s.cancel()};return append([]byte(nil),s.body...),nil}

func TestCompileImportPlanRetainsUnsupportedEntryAndReasons(t *testing.T){
 workflow,project,ref,parsed,sources:=importSourcesFixture(t);raw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
 frozen,err:=freezeImportSources(context.Background(),workflow,project,ref,parsed,raw);if err!=nil{t.Fatal(err)}
 compiled,err:=CompileImportPlan(context.Background(),frozen,parsed,rawSHA([]byte("snapshot")),ImportPlanLimits{1024,2048,10},nil,nil);if err!=nil{t.Fatal(err)}
 var plan importPlanDocument;if err:=decodeImportDocument(compiled.Body(),&plan);err!=nil{t.Fatal(err)}
 if len(plan.Entries)!=1||plan.Entries[0].ID!=sources.Entries[0].ID||plan.Entries[0].Binding!=nil||plan.Entries[0].Recipe!=nil||string(compiled.SourcesBody())!=string(raw){t.Fatal("terminal entry/evidence lost")}
 body:=compiled.Body();body[0]='x';if compiled.Body()[0]!='{'{t.Fatal("compiled body aliases caller")}
 if _,err:=compiled.Documents(ImportDocumentIdentity{Run:uuid.New(),Workflow:workflow});err!=nil{t.Fatal(err)}
 if _,err:=compiled.Documents(ImportDocumentIdentity{Run:uuid.New(),Workflow:uuid.New()});err==nil{t.Fatal("foreign workflow accepted")}
}

func TestCompileImportPlanQualifiesAdoptedMaster(t *testing.T){
 _,masterEntry,masterRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(masterRaw);if err!=nil{t.Fatal(err)}
 parsed:=importParsedValidatorFixture();parsed.Manifest.Entries[0].ImageSHA=image.Canvas.SourceSHA256;parsed.Manifest.Inventory[0].SHA=image.Canvas.SourceSHA256;parsed.Files[0].Content.SHA256=image.Canvas.SourceSHA256
 parsedRaw,err:=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
 workflow,project,version:=uuid.New(),uuid.New(),uuid.New();run:=uuid.NewSHA1(workflow,[]byte("interchange:canonical-ingest:1"));ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(parsedRaw)}
 sources:=importSourcesDocument{Schema:1,Workflow:workflow,Project:project,Parsed:ref,IngestRun:&run,Entries:[]importSourceEntry{{ID:parsed.Manifest.Entries[0].ID,State:"adopted",Version:&version,Master:&masterEntry.Ref}}}
 raw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)};frozen,err:=freezeImportSources(context.Background(),workflow,project,ref,parsedRaw,raw);if err!=nil{t.Fatal(err)}
 for _,change:=range []string{"none","hash","cancel","missing_reader"}{t.Run(change,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();reader:=&importMasterReaderStub{body:masterRaw};var port ImportMasterReader=reader
  if change=="hash"{reader.body=[]byte(`{}`)};if change=="cancel"{reader.cancel=cancel};if change=="missing_reader"{port=nil}
  result,err:=CompileImportPlan(ctx,frozen,parsedRaw,rawSHA([]byte("snapshot")),ImportPlanLimits{1024,2048,10},port,nil)
  if change!="none"{if err==nil{t.Fatal("invalid master accepted")};if change=="cancel"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)};return}
  if err!=nil{t.Fatal(err)};var plan importPlanDocument;if err:=decodeImportDocument(result.Body(),&plan);err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  if plan.Entries[0].Binding==nil||*plan.Entries[0].Binding!=(v2input.SessionVersionBinding{AssetVersionID:version,Source:masterEntry.Ref,CanvasHash:canvas}){t.Fatal("canonical binding mismatch")}
 })}
}
