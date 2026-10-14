package interchange

import (
 "context"
 "errors"
 "fmt"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type importRecipeResolverFunc func(context.Context,uuid.UUID,v2input.SessionVersionBinding)(*v2input.Ref,error)
func(f importRecipeResolverFunc)ResolvePreparedImportRecipe(ctx context.Context,id uuid.UUID,binding v2input.SessionVersionBinding)(*v2input.Ref,error){return f(ctx,id,binding)}

func importAdoptedRecipeFixture(t *testing.T)(ImportSourceInventory,[]byte,[]byte){
 _,master,masterRaw:=importWorkingValidatorFixture(t)
 image,err:=review.ParseWorkingImage(masterRaw);if err!=nil{t.Fatal(err)}
 parsed:=importParsedValidatorFixture()
 parsed.Manifest.Entries[0].ImageSHA=image.Canvas.SourceSHA256
 parsed.Manifest.Inventory[0].SHA=image.Canvas.SourceSHA256
 parsed.Files[0].Content.SHA256=image.Canvas.SourceSHA256
 path,recipeSHA:="recipes/frame.json",rawSHA([]byte("archived recipe"))
 parsed.Manifest.Entries[0].RecipePath=&path;parsed.Manifest.Entries[0].RecipeSHA=&recipeSHA
 parsed.Manifest.Inventory=append(parsed.Manifest.Inventory,importBundleInventory{ID:uuid.New(),Path:path,SHA:recipeSHA,Contract:"EditingRecipeV3"})
 parsed.Files=append(parsed.Files,importParsedFile{Path:path,Content:v2input.Ref{ArtifactID:uuid.New(),SHA256:recipeSHA},Contract:"EditingRecipeV3"})
 raw,err:=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
 workflow,project,version:=uuid.New(),uuid.New(),uuid.New()
 run:=uuid.NewSHA1(workflow,[]byte("interchange:canonical-ingest:1"))
 sources:=importSourcesDocument{Schema:1,Workflow:workflow,Project:project,Parsed:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},IngestRun:&run,Entries:[]importSourceEntry{{Ordinal:0,ID:parsed.Manifest.Entries[0].ID,State:"adopted",Version:&version,Master:&master.Ref}}}
 sourceRaw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
 frozen,err:=freezeImportSources(context.Background(),workflow,project,sources.Parsed,raw,sourceRaw);if err!=nil{t.Fatal(err)}
 return frozen,raw,masterRaw
}

func TestImportPlanCapturesExplicitUnsupportedReasonAndPreservesRealFailures(t *testing.T){
 for _,outcome:=range []string{"unsupported","supported","nil_receipt","empty_reason","long_reason","invalid_utf8","ambiguous","transport","cancelled"}{t.Run(outcome,func(t *testing.T){
  sources,parsed,master:=importAdoptedRecipeFixture(t)
  sentinel:=errors.New("object reader unavailable")
  receipt:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("prepared recipe"))}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  resolver:=importRecipeResolverFunc(func(_ context.Context,id uuid.UUID,binding v2input.SessionVersionBinding)(*v2input.Ref,error){
   if id!=sources.document.Entries[0].ID||binding.AssetVersionID!=*sources.document.Entries[0].Version||binding.Source!=*sources.document.Entries[0].Master{t.Fatal("wrong recipe binding")}
   switch outcome{
   case "supported":return &receipt,nil
   case "nil_receipt":return nil,nil
   case "transport":return nil,sentinel
   case "cancelled":cancel();return nil,&ImportRecipeUnsupported{Reason:"unsupported_layer"}
   case "empty_reason":return nil,&ImportRecipeUnsupported{}
   case "long_reason":return nil,&ImportRecipeUnsupported{Reason:strings.Repeat("x",201)}
   case "invalid_utf8":return nil,&ImportRecipeUnsupported{Reason:string([]byte{0xff})}
   case "ambiguous":return &receipt,&ImportRecipeUnsupported{Reason:"unsupported_layer"}
   default:return nil,fmt.Errorf("compiler: %w",&ImportRecipeUnsupported{Reason:"unsupported_layer"})
   }
  })
  plan,err:=CompileImportPlan(ctx,sources,parsed,rawSHA([]byte("snapshot")),ImportPlanLimits{1024,2048,10},&importMasterReaderStub{body:master},resolver)
  switch outcome{
  case "transport":if !errors.Is(err,sentinel){t.Fatal("transport failure reclassified",err)};return
  case "cancelled":if !errors.Is(err,context.Canceled){t.Fatal("cancel reclassified",err)};return
  case "supported","unsupported":if err!=nil{t.Fatal(err)}
  default:if err==nil{t.Fatal("ambiguous/invalid compiler outcome accepted")};return
  }
  raw,err:=plan.DiagnosticsBody();if err!=nil{t.Fatal(err)}
  var diagnostics []importRecipeDiagnostic;if err:=decodeImportDocument(raw,&diagnostics);err!=nil{t.Fatal(err)}
  if outcome=="unsupported"&&(len(diagnostics)!=1||diagnostics[0].ID!=sources.document.Entries[0].ID||diagnostics[0].Reason!="unsupported_layer"){t.Fatal("compiler diagnostic lost")}
  if outcome=="supported"&&len(diagnostics)!=0{t.Fatal("supported recipe marked unsupported")}
  raw[0]='x';again,err:=plan.DiagnosticsBody();if err!=nil||again[0]!='['{t.Fatal("diagnostics alias mutable caller buffer",err)}
 })}
}

func TestExecutableImportUsesCapturedCompilerDiagnosticsWithoutExternalReader(t *testing.T){
 sources,parsed,master:=importAdoptedRecipeFixture(t)
 original:=sources.document.Entries[0]
 metadata:=importMetadataDocument{Schema:1,Workflow:sources.document.Workflow,Project:sources.document.Project,Parsed:sources.document.Parsed,Collection:uuid.NewSHA1(sources.document.Workflow,[]byte("interchange:draft-collection:1")),Entries:[]importMetadataReceipt{{ID:original.ID,Version:*original.Version,Master:*original.Master,Item:uuid.NewSHA1(*sources.document.IngestRun,[]byte("item:0")),Rating:0,Flag:"none"}}}
 raw,err:=canonical.Marshal(metadata);if err!=nil{t.Fatal(err)}
 compiler:=ImportExecutionCompiler{Plan:ImportPlanPreparation{Sources:&importSourceReaderStub{inventory:sources},Masters:&importMasterReaderStub{body:master},Recipes:importRecipeResolverFunc(func(context.Context,uuid.UUID,v2input.SessionVersionBinding)(*v2input.Ref,error){return nil,&ImportRecipeUnsupported{Reason:"unsupported_layer"}})},Metadata:&importMetadataReaderStub{body:raw}}
 documents,err:=compiler.Compile(context.Background(),ImportDocumentIdentity{Run:uuid.New(),Workflow:sources.document.Workflow},sources.document.Parsed,parsed,rawSHA([]byte("snapshot")),ImportPlanLimits{1024,2048,10});if err!=nil{t.Fatal(err)}
 var evidence importEvidenceDocument;if err:=decodeImportDocument(documents.evidence.Body,&evidence);err!=nil{t.Fatal(err)}
 if len(evidence.Diagnostics)!=1||evidence.Diagnostics[0].ID!=original.ID||evidence.Diagnostics[0].Reason!="unsupported_layer"{t.Fatal("missing captured outcome")}
}
