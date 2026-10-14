package interchange

import (
 "bytes"
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type importMetadataReaderStub struct{body []byte;calls int;changed bool;failure error;afterRead func()}
func(s *importMetadataReaderStub)ReadImportMetadata(_ context.Context,_ v2input.Ref,_ []byte)([]byte,error){
 s.calls++
 if s.calls>1&&s.failure!=nil{return nil,s.failure}
 if s.afterRead!=nil{s.afterRead()}
 if s.calls>1&&s.changed{return []byte(`{}`),nil}
 return s.body,nil
}
type importDiagnosticsReaderFunc func(context.Context,[]byte,[]byte)([]byte,error)
func(f importDiagnosticsReaderFunc)ReadImportRecipeDiagnostics(ctx context.Context,plan,parsed []byte)([]byte,error){return f(ctx,plan,parsed)}

func TestImportExecutionCompilerRechecksCurrentReceiptsAndOwnsEvidence(t *testing.T){
 for _,change:=range []string{"none","format_xmp","metadata_changed","metadata_denied","sources_changed","foreign_metadata","extra_diagnostic","cancelled"}{t.Run(change,func(t *testing.T){
  workflow,project,ref,parsed,sources:=importSourcesFixture(t)
  if change=="format_xmp"{sources.Format="xmp"}
  raw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
  frozen,err:=freezeImportSources(context.Background(),workflow,project,ref,parsed,raw);if err!=nil{t.Fatal(err)}
  sourceReader:=&importSourceReaderStub{inventory:frozen}
  metadata:=importMetadataDocument{Schema:1,Workflow:workflow,Project:project,Parsed:ref,Collection:uuid.NewSHA1(workflow,[]byte("interchange:draft-collection:1")),Entries:[]importMetadataReceipt{}}
  if change=="foreign_metadata"{metadata.Project=uuid.New()}
  metadataRaw,err:=canonical.Marshal(metadata);if err!=nil{t.Fatal(err)}
  reader:=&importMetadataReaderStub{body:metadataRaw,changed:change=="metadata_changed"}
  denied:=errors.New("metadata authorization revoked");if change=="metadata_denied"{reader.failure=denied}
  compiler:=ImportExecutionCompiler{Plan:ImportPlanPreparation{Sources:sourceReader},Metadata:reader}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  compiler.Diagnostics=importDiagnosticsReaderFunc(func(_ context.Context,plan,parsed []byte)([]byte,error){
   // Mutating port-owned argument buffers must not alter the compiled documents.
   plan[0]='x';parsed[0]='x'
   if change=="sources_changed"{sourceReader.change=true}
   if change=="cancelled"{cancel()}
   if change=="extra_diagnostic"{return canonical.Marshal([]importRecipeDiagnostic{{ID:sources.Entries[0].ID,Reason:"not applicable"}})}
   return []byte(`[]`),nil
  })
  documents,err:=compiler.Compile(ctx,ImportDocumentIdentity{Run:uuid.New(),Workflow:workflow},ref,parsed,rawSHA([]byte("snapshot")),ImportPlanLimits{1024,2048,10})
  switch change{
  case "none","format_xmp":
   if err!=nil{t.Fatal(err)}
   if reader.calls!=2||sourceReader.calls!=3||documents.evidence==nil{t.Fatal("current receipts were not rechecked")}
   if bytes.Contains(documents.evidence.Body,[]byte(`"format":"photoeditor_bundle"`))||bytes.Contains(documents.evidence.Body,[]byte(`"format":"xmp"`))!=(change=="format_xmp"){t.Fatal("source format evidence wire changed")}
   metadataRaw[0]='x'
   var proof importEvidenceDocument;if err:=decodeImportDocument(documents.evidence.Body,&proof);err!=nil{t.Fatal(err)}
   if proof.Project!=project||proof.Sources.Entries[0].State!="unsupported"||*proof.Sources.Entries[0].Reason!="unsupported_image_format"{t.Fatal("terminal evidence lost")}
   var params importParamsDocument;if err:=decodeImportDocument(documents.params.Body,&params);err!=nil||params.Evidence==nil||*params.Evidence!=documents.evidence.Ref{t.Fatal("executable evidence missing",err)}
  case "metadata_changed","sources_changed":if !errors.Is(err,artifactjson.ErrStale){t.Fatal("snapshot race accepted",err)}
  case "metadata_denied":if !errors.Is(err,denied){t.Fatal("revocation lost",err)}
  case "cancelled":if !errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
  default:if err==nil{t.Fatal("foreign/extraneous evidence accepted")}
  }
 })}
}

func TestImportExecutionCompilerRejectsMissingMetadataBeforeIO(t *testing.T){
 reader:=&importSourceReaderStub{}
 compiler:=ImportExecutionCompiler{Plan:ImportPlanPreparation{Sources:reader}}
 if _,err:=compiler.Compile(context.Background(),ImportDocumentIdentity{Run:uuid.New(),Workflow:uuid.New()},v2input.Ref{},nil,"",ImportPlanLimits{});err==nil||reader.calls!=0{t.Fatal("missing metadata reader reached compilation")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=compiler.Compile(ctx,ImportDocumentIdentity{},v2input.Ref{},nil,"",ImportPlanLimits{});!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
}
