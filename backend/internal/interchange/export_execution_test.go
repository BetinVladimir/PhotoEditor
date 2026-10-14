package interchange
import (
 "context"
 "testing"
 "strings"
 "photoeditor/backend/internal/artifactjson"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
type exportRegistrarStub struct{calls int}
func(s *exportRegistrarStub)RegisterExportInputs(context.Context,CreateIntent,FrozenExportDocuments,ExportTransport)error{s.calls++;return nil}
func TestExportExecutionRejectsIncompleteDependenciesBeforeIO(t *testing.T){
 documents:=&exportPreparationStub{};transport:=&exportScopedDocuments{};registrar:=&exportRegistrarStub{}
 resolver:=&exportNodeResolverStub{}
 for _,mode:=range []string{"documents","resolver","materializer","transport","registrar"}{t.Run(mode,func(t *testing.T){
  p:=ExportExecutionPreparation{Documents:documents,Resolver:resolver,Materializer:documents,TransportDocuments:transport,Registrar:registrar}
  switch mode{case "documents":p.Documents=nil;case "resolver":p.Resolver=nil;case "materializer":p.Materializer=nil;case "transport":p.TransportDocuments=nil;case "registrar":p.Registrar=nil}
  if _,err:=p.Prepare(context.Background(),auth.Identity{},uuid.Nil,CreateRequest{},"key",FrozenExportDocuments{},ExportTaskResources{});err==nil{t.Fatal("incomplete preparation accepted")}
  if len(documents.purposes)!=0||len(transport.scopes)!=0||documents.staged!=0||registrar.calls!=0||resolver.calls!=0{t.Fatal("incomplete preparation performed IO")}
 })}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(ExportExecutionPreparation{}).Prepare(ctx,auth.Identity{},uuid.Nil,CreateRequest{},"key",FrozenExportDocuments{},ExportTaskResources{});err!=context.Canceled{t.Fatal("cancellation ignored")}
}
func TestExportInputRegistrarRequiresDatabase(t *testing.T){
 if err:=(SQLExportInputRegistrar{}).RegisterExportInputs(context.Background(),CreateIntent{},FrozenExportDocuments{},ExportTransport{});err!=v2input.ErrInvalid{t.Fatal("missing database accepted",err)}
}

type exportPipelineStub struct{events []string;fail string;resolver *exportNodeResolverStub;mutate bool}
func(s *exportPipelineStub)PublishExportDocument(_ context.Context,_ CreateIdentity,doc CreateDocument)(v2input.Ref,error){
 s.events=append(s.events,doc.Purpose);if s.fail==doc.Purpose{return v2input.Ref{},v2input.ErrInvalid};return doc.Ref,nil
}
func(s *exportPipelineStub)ResolveExportNode(ctx context.Context,entry v2input.Entry)(ExportClosureNode,error){
 if s.fail=="resolver"{return ExportClosureNode{},v2input.ErrInvalid};return s.resolver.ResolveExportNode(ctx,entry)
}
func(s *exportPipelineStub)MaterializeExport(_ context.Context,intent CreateIntent,_ FrozenExportDocuments,_ v2input.DraftTask)error{
 s.events=append(s.events,"materialize");if s.mutate{*intent.Request.SnapshotID=uuid.New();intent.RequestJCS[0]='!'}
 if s.fail=="materialize"{return v2input.ErrInvalid};return nil
}
func(s *exportPipelineStub)Publish(_ context.Context,_ artifactjson.Scope,body []byte)(artifactjson.Ref,error){
 s.events=append(s.events,"transport");if s.fail=="transport"{return artifactjson.Ref{},v2input.ErrInvalid};return artifactjson.Ref{ID:uuid.New(),SHA256:rawSHA(body)},nil
}
func(s *exportPipelineStub)RegisterExportInputs(_ context.Context,_ CreateIntent,_ FrozenExportDocuments,_ ExportTransport)error{
 s.events=append(s.events,"register");if s.fail=="register"{return v2input.ErrInvalid};return nil
}
func exportExecutionFixture(t *testing.T)(uuid.UUID,auth.Identity,CreateIntent,FrozenExportDocuments,*exportNodeResolverStub){
 old,resolver:=exportClosureFixture(t)
 var source importPlanDocument;if err:=decodeImportDocument(old.planBody,&source);err!=nil{t.Fatal(err)}
 var snapshotRef v2input.Ref;var snapshotRaw []byte
 for _,root:=range old.roots{if root.Contract=="InputSnapshot"{snapshotRef=root.Ref;snapshotRaw=resolver.nodes[root.Ref.ArtifactID].Body}}
 var snapshot v2input.SessionInputSnapshot;if err:=decodeImportDocument(snapshotRaw,&snapshot);err!=nil{t.Fatal(err)}
 tid:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};sid:=snapshot.SnapshotID
 intent,err:=FreezeCreateIntent(tid,who,CreateRequest{ProjectID:source.Project,Direction:"export",Format:source.Format,SnapshotID:&sid},"pipeline-key");if err!=nil{t.Fatal(err)}
 entry:=source.Entries[0]
 compiled,err:=CompileExportPlan(context.Background(),intent.Identity.Workflow,source.Project,source.Format,snapshotRef,snapshotRaw,ExportLimits{source.Archive,source.Expanded,source.Files},exportFrameResolverFunc(func(context.Context,v2input.SessionSnapshotItem)(ExportFrame,error){return ExportFrame{ExternalID:entry.External,Path:entry.Path,Content:entry.Content,PortableRecipe:*entry.Recipe,Rating:entry.Rating,Pick:entry.Pick,Crop:entry.Crop},nil}));if err!=nil{t.Fatal(err)}
 byID:=map[uuid.UUID]v2input.Entry{};for _,root:=range compiled.roots{byID[root.Ref.ArtifactID]=root}
 for _,document:=range []struct{ref v2input.Ref;body []byte;contract string}{{compiled.planRef,compiled.planBody,"InterchangePlan"},{compiled.paramsRef,compiled.paramsBody,"Params"}}{
  refs,err:=importDocumentRefs(document.body,document.contract);if err!=nil{t.Fatal(err)};children:=[]v2input.Entry{};for id:=range refs{children=append(children,byID[id])}
  resolver.nodes[document.ref.ArtifactID]=ExportClosureNode{Entry:byID[document.ref.ArtifactID],Body:document.body,Children:children}
 }
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshotRef,snapshotRaw);if err!=nil{t.Fatal(err)}
 return tid,who,intent,frozen,resolver
}
func TestExportExecutionCompleteSequenceAndStageFailures(t *testing.T){
 for _,mode:=range []string{"success","v30_interchange_export_plan","task_params","resolver","materialize","transport","register","mutation"}{t.Run(mode,func(t *testing.T){
  tid,who,intent,frozen,resolver:=exportExecutionFixture(t)
  stub:=&exportPipelineStub{fail:mode,resolver:resolver,mutate:mode=="mutation"}
  p:=ExportExecutionPreparation{Documents:stub,Resolver:stub,Materializer:stub,TransportDocuments:stub,Registrar:stub}
  resources:=ExportTaskResources{RAMBytes:2<<30,ScratchBytes:2<<30,OutputBytes:1<<30,MaxInputJSONBytes:128<<20,ContractsSHA:rawSHA([]byte("schemas"))}
  result,err:=p.Prepare(context.Background(),who,tid,intent.Request,"pipeline-key",frozen,resources)
  if mode=="success"||mode=="mutation"{
   if err!=nil||result.TaskID()!=intent.Identity.Task{t.Fatal("complete sequence failed",err)}
   if strings.Join(stub.events,",")!="v30_interchange_export_plan,task_params,materialize,transport,transport,register"{t.Fatal("incorrect stage ordering",stub.events)}
  }else{
   if err==nil||result.TaskID()!=uuid.Nil{t.Fatal("failed stage exposed transport")}
   if mode!="register"&&len(stub.events)>0&&stub.events[len(stub.events)-1]=="register"{t.Fatal("failure reached registrar",stub.events)}
  }
 })}
}

func TestExportResourceRefusalPreventsMaterializationAndTransport(t *testing.T){
 for _,mode:=range []string{"refused","different","mutation"}{t.Run(mode,func(t *testing.T){
  tid,who,intent,frozen,resolver:=exportExecutionFixture(t)
  resources:=ExportTaskResources{RAMBytes:2<<30,ScratchBytes:2<<30,OutputBytes:1<<30,MaxInputJSONBytes:128<<20,ContractsSHA:rawSHA([]byte("schemas"))}
  qualifier:=&exportResourceQualifierStub{resources:resources}
  switch mode{case "refused":qualifier.err=v2input.ErrInvalid;case "different":qualifier.resources.RAMBytes++;case "mutation":qualifier.mutate=true}
  stub:=&exportPipelineStub{resolver:resolver}
  preparation:=ExportExecutionPreparation{Documents:stub,Resolver:stub,Materializer:stub,TransportDocuments:stub,Registrar:stub,Qualification:qualifier}
  result,err:=preparation.Prepare(context.Background(),who,tid,intent.Request,"pipeline-key",frozen,resources)
  if qualifier.calls!=1{t.Fatal("qualification not called before materialization")}
  if mode=="mutation"{if err!=nil||result.TaskID()!=intent.Identity.Task{t.Fatal("qualification alias affected execution",err)}}else{
   if err==nil||result.TaskID()!=uuid.Nil{t.Fatal("unqualified task accepted")}
   if strings.Join(stub.events,",")!="v30_interchange_export_plan,task_params"{t.Fatal("refusal reached materialization/transport",stub.events)}
  }
 })}
}
