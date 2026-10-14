package interchange

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func importSourcesFixture(t *testing.T)(uuid.UUID,uuid.UUID,v2input.Ref,[]byte,importSourcesDocument){
 parsed:=importParsedValidatorFixture();raw,err:=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
 workflow,project:=uuid.New(),uuid.New();ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)};reason:="unsupported_image_format"
 sources:=importSourcesDocument{Schema:1,Workflow:workflow,Project:project,Parsed:ref,Entries:[]importSourceEntry{{Ordinal:0,ID:parsed.Manifest.Entries[0].ID,State:"unsupported",Reason:&reason}}}
 return workflow,project,ref,raw,sources
}

func TestImportSourcesOwnTerminalEvidenceWithoutInventedBindings(t *testing.T){
 workflow,project,ref,parsed,sources:=importSourcesFixture(t)
 raw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
 frozen,err:=freezeImportSources(context.Background(),workflow,project,ref,parsed,raw)
 if err!=nil||frozen.document.Format!="photoeditor_bundle"||frozen.document.Entries[0].State!="unsupported"||frozen.document.Entries[0].Master!=nil||*frozen.document.Entries[0].Reason!="unsupported_image_format"{t.Fatal("terminal evidence",err)}
 expected:=string(raw);raw[0]='x';copy:=frozen.Body();copy[0]='x'
 if string(frozen.Body())!=expected{t.Fatal("snapshot aliased caller bytes")}
}
func TestImportSourcesPreserveFormatIntoCompiledPlan(t *testing.T){
 for _,format:=range []string{"photoeditor_bundle","xmp","captureone_sidecar"}{t.Run(format,func(t *testing.T){
  workflow,project,ref,parsed,sources:=importSourcesFixture(t)
  sources.Format=format
  raw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
  frozen,err:=freezeImportSources(context.Background(),workflow,project,ref,parsed,raw);if err!=nil{t.Fatal(err)}
  compiled,err:=CompileImportPlan(context.Background(),frozen,parsed,rawSHA([]byte("snapshot")),ImportPlanLimits{ArchiveBytes:1024,ExpandedBytes:2048,Files:10},nil,nil);if err!=nil{t.Fatal(err)}
  var plan importPlanDocument
  if err:=decodeImportDocument(compiled.Body(),&plan);err!=nil||plan.Format!=format{t.Fatal("import format lost",err,plan.Format)}
 })}
 workflow,project,ref,parsed,sources:=importSourcesFixture(t)
 sources.Format="unsupported"
 raw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
 if _,err:=freezeImportSources(context.Background(),workflow,project,ref,parsed,raw);err==nil{t.Fatal("unknown import format accepted")}
}
func TestSidecarFormatChangesPublishedPlanAndParamsIdentity(t *testing.T){
 workflow,project,ref,parsed,base:=importSourcesFixture(t)
 identity:=ImportDocumentIdentity{Run:uuid.New(),Workflow:workflow}
 var prior ImportPreparedDocuments
 for ordinal,format:=range []string{"xmp","captureone_sidecar"}{
  sources:=base;sources.Format=format
  raw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
  frozen,err:=freezeImportSources(context.Background(),workflow,project,ref,parsed,raw);if err!=nil{t.Fatal(err)}
  compiled,err:=CompileImportPlan(context.Background(),frozen,parsed,rawSHA([]byte("snapshot")),ImportPlanLimits{ArchiveBytes:1024,ExpandedBytes:2048,Files:10},nil,nil);if err!=nil{t.Fatal(err)}
  planBody:=compiled.Body()
  documents,err:=FreezeImportDocuments(identity,ref,planBody);if err!=nil{t.Fatal(err)}
  planBody[0]='x'
  var pinned importPlanDocument
  if err:=decodeImportDocument(documents.plan.Body,&pinned);err!=nil||pinned.Format!=format{t.Fatal("published format changed",err,pinned.Format)}
  if ordinal>0&&(documents.plan.Ref.SHA256==prior.plan.Ref.SHA256||documents.params.Ref.SHA256==prior.params.Ref.SHA256){t.Fatal("format substitution reused document identities")}
  prior=documents
 }
}

func TestImportSourcesRejectScopeOrderStateAndIngestSubstitution(t *testing.T){
 for _,change:=range []string{"workflow","project","parsed","ordinal","entry","count","state","reason","binding","missing_ingest","foreign_ingest","spurious_ingest","missing_master"}{t.Run(change,func(t *testing.T){
  workflow,project,ref,parsed,sources:=importSourcesFixture(t)
  switch change{
  case "workflow":sources.Workflow=uuid.New()
  case "project":sources.Project=uuid.New()
  case "parsed":sources.Parsed.ArtifactID=uuid.New()
  case "ordinal":sources.Entries[0].Ordinal=1
  case "entry":sources.Entries[0].ID=uuid.New()
  case "count":sources.Entries=nil
  case "state":sources.Entries[0].State="pending"
  case "reason":sources.Entries[0].Reason=nil
  case "binding":version:=uuid.New();sources.Entries[0].Version=&version
  case "missing_ingest":sources.Entries[0].State="rejected"
  case "foreign_ingest":run:=uuid.New();sources.IngestRun=&run
  case "spurious_ingest":run:=uuid.NewSHA1(workflow,[]byte("interchange:canonical-ingest:1"));sources.IngestRun=&run
  case "missing_master":run:=uuid.NewSHA1(workflow,[]byte("interchange:canonical-ingest:1"));sources.IngestRun=&run;sources.Entries[0].State="adopted";sources.Entries[0].Reason=nil;version:=uuid.New();sources.Entries[0].Version=&version
  }
  raw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
  if _,err:=freezeImportSources(context.Background(),workflow,project,ref,parsed,raw);err==nil{t.Fatal("invalid source inventory accepted")}
 })}
}

func TestImportSourcesAdoptedAndRejectedRequireExactChildRun(t *testing.T){
 for _,state:=range []string{"adopted","rejected"}{
  workflow,project,ref,parsed,sources:=importSourcesFixture(t);run:=uuid.NewSHA1(workflow,[]byte("interchange:canonical-ingest:1"));sources.IngestRun=&run;sources.Entries[0].State=state
  if state=="adopted"{version:=uuid.New();sources.Entries[0].Version=&version;sources.Entries[0].Master=&v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("master"))};sources.Entries[0].Reason=nil}
  raw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
  if _,err:=freezeImportSources(context.Background(),workflow,project,ref,parsed,raw);err!=nil{t.Fatal("valid child evidence",err)}
  ctx,cancel:=context.WithCancel(context.Background());cancel()
  if _,err:=freezeImportSources(ctx,workflow,project,ref,parsed,raw);!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
 }
}
