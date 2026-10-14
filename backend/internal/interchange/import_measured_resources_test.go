package interchange
import (
 "testing"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/canonical"
)
func TestImportMeasurementsRequireIndependentPinsAndSafeBounds(t *testing.T){
 for _,mode:=range []string{"valid","runtime","hash","duplicate","ram","vocabulary"}{t.Run(mode,func(t *testing.T){
  digest:=func(s string)string{return rawSHA([]byte(s))}
  m:=ImportResourceMeasurement{DocumentsSHA:digest("documents"),InputDigest:digest("inputs"),RAM:1<<30,Scratch:0,Output:1<<20}
  report:=ImportResourceMeasurements{Schema:"interchange-import-resource-measurements/v1",Runtime:digest("runtime"),Contracts:digest("contracts"),Hardware:digest("hardware"),Measurements:[]ImportResourceMeasurement{m}}
  switch mode{case "runtime":report.Runtime=digest("other");case "duplicate":report.Measurements=append(report.Measurements,m);case "ram":report.Measurements[0].RAM=1;case "vocabulary":report.Schema="interchange-export-resource-measurements/v1"}
  raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)};pin:=rawSHA(raw);if mode=="hash"{pin=digest("other")}
  q,err:=NewMeasuredImportResourceQualifier(raw,pin,digest("runtime"),digest("contracts"),digest("hardware"))
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected measured report decision",err)}
  if mode!="valid"&&q!=nil{t.Fatal("partial qualifier returned")}
 })}
}

func measuredImportFixtureWithParsed(t *testing.T)(ImportPreparedDocuments,v2input.DraftTask,ImportInputInventory,[]byte){
 workflow,project,parsedRef,parsedRaw,sources:=importSourcesFixture(t)
 sourcesRaw,err:=canonical.Marshal(sources);if err!=nil{t.Fatal(err)}
 frozen,err:=freezeImportSources(context.Background(),workflow,project,parsedRef,parsedRaw,sourcesRaw);if err!=nil{t.Fatal(err)}
 metadata,err:=canonical.Marshal(importMetadataDocument{Schema:1,Workflow:workflow,Project:project,Parsed:parsedRef,Collection:uuid.NewSHA1(workflow,[]byte("interchange:draft-collection:1")),Entries:[]importMetadataReceipt{}});if err!=nil{t.Fatal(err)}
 compiler:=ImportExecutionCompiler{Plan:ImportPlanPreparation{Sources:&importSourceReaderStub{inventory:frozen}},Metadata:&importMetadataReaderStub{body:metadata}}
 docs,err:=compiler.Compile(context.Background(),ImportDocumentIdentity{Run:uuid.New(),Workflow:workflow},parsedRef,parsedRaw,rawSHA([]byte("snapshot")),ImportPlanLimits{1024,2048,10});if err!=nil{t.Fatal(err)}
 entries:=[]v2input.Entry{{Contract:"Params",Ref:docs.params.Ref},{Contract:"InterchangePlan",Ref:docs.plan.Ref},{Contract:"ParsedInterchange",Ref:parsedRef},{Contract:"FrozenInterchangeImportEvidence",Ref:docs.evidence.Ref}}
 inventory,err:=FreezeImportInputs(uuid.New(),docs,entries);if err!=nil{t.Fatal(err)}
 _,task:=importMaterializationFixture(t);task.ID=docs.TaskID();task.ParamsArtifactID=docs.params.Ref.ArtifactID;task.ParamsSHA=docs.params.Ref.SHA256;task.InputDigest=inventory.InputDigest()
 return docs,task,inventory,parsedRaw
}
func TestMeasuredImportMatchesExactDocumentsInventoryAndProfile(t *testing.T){
 for _,mode:=range []string{"valid","documents","inventory","profile","missing_measurement","cancelled"}{t.Run(mode,func(t *testing.T){
  docs,task,inventory:=measuredImportFixture(t)
  frozen,err:=importResourceDocumentsSHA(docs);if err!=nil{t.Fatal(err)}
  digest:=func(s string)string{return rawSHA([]byte(s))}
  report:=ImportResourceMeasurements{Schema:"interchange-import-resource-measurements/v1",Runtime:digest("runtime"),Contracts:digest("contracts"),Hardware:digest("hardware"),Measurements:[]ImportResourceMeasurement{{DocumentsSHA:frozen,InputDigest:inventory.InputDigest(),RAM:1<<30,Scratch:0,Output:25165824}}}
  if mode=="missing_measurement"{report.Measurements[0].DocumentsSHA=digest("other")}
  raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
  q,err:=NewMeasuredImportResourceQualifier(raw,rawSHA(raw),report.Runtime,report.Contracts,report.Hardware);if err!=nil{t.Fatal(err)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{
  case "documents":docs.evidence.Body=append(docs.evidence.Body,' ')
  case "inventory":inventory.entries[0].Ref.SHA256=digest("other")
  case "profile":task.ExecutionProfile=json.RawMessage(`{"backend":"cpu","renderer_version":"v2.2","resource_estimate":{"ram_bytes":2147483648,"vram_bytes":0,"scratch_bytes":0,"output_bytes":25165824}}`)
  case "cancelled":cancel()
  }
  err=q.QualifyImportActivationResources(ctx,docs,task,inventory)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected measured import admission",err)}
 })}
}

func measuredImportFixture(t *testing.T)(ImportPreparedDocuments,v2input.DraftTask,ImportInputInventory){
 docs,task,inventory,_:=measuredImportFixtureWithParsed(t);return docs,task,inventory
}


func TestMeasuredImportBuildsExactTaskWithoutRequestEstimates(t *testing.T){
 for _,mode:=range []string{"valid","missing_measurement","changed_inventory","changed_documents","cancelled"}{t.Run(mode,func(t *testing.T){
  docs,_,inventory:=measuredImportFixture(t)
  frozen,err:=importResourceDocumentsSHA(docs);if err!=nil{t.Fatal(err)}
  pin:=func(s string)string{return rawSHA([]byte(s))}
  report:=ImportResourceMeasurements{Schema:"interchange-import-resource-measurements/v1",Runtime:pin("runtime"),Contracts:pin("contracts"),Hardware:pin("hardware"),Measurements:[]ImportResourceMeasurement{{DocumentsSHA:frozen,InputDigest:inventory.InputDigest(),RAM:2<<30,Scratch:16<<20,Output:32<<20}}}
  if mode=="missing_measurement"{report.Measurements[0].DocumentsSHA=pin("other")}
  raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
  q,err:=NewMeasuredImportResourceQualifier(raw,rawSHA(raw),report.Runtime,report.Contracts,report.Hardware);if err!=nil{t.Fatal(err)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{case "changed_inventory":inventory.entries[0].Ref.SHA256=pin("changed");case "changed_documents":docs.evidence.Body=append(docs.evidence.Body,' ');case "cancelled":cancel()}
  task,err:=q.BuildImportTask(ctx,docs,inventory)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected measured task decision",err)}
  if mode!="valid"{if task.ID!=uuid.Nil{t.Fatal("failed task build returned identity")};return}
  if task.ID!=docs.TaskID()||task.InputDigest!=inventory.InputDigest()||task.ParamsSHA!=docs.params.Ref.SHA256{t.Fatal("task identity lost")}
  if err:=q.QualifyImportActivationResources(ctx,docs,task,inventory);err!=nil{t.Fatal(err)}
  task.ExecutionProfile[0]='!'
  again,err:=q.BuildImportTask(ctx,docs,inventory);if err!=nil||again.ExecutionProfile[0]!='{'{t.Fatal("profile buffer reused",err)}
 })}
}
