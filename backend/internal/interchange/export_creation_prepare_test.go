package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
type exportCreationAdapters struct{frozen FrozenExportDocuments;installation ExportExecutionInstallation;events []string;fail string;cause error;mutate bool}
func(s *exportCreationAdapters)ProposeExportDocuments(_ context.Context,_ auth.Identity,_ uuid.UUID,request CreateRequest,_ string)(FrozenExportDocuments,error){
 s.events=append(s.events,"proposal");if s.mutate{*request.SnapshotID=uuid.New()};if s.fail=="proposal"{return FrozenExportDocuments{},s.cause};return s.frozen,nil
}
func(s *exportCreationAdapters)PrepareExportDraft(_ context.Context,_ auth.Identity,_ uuid.UUID,_ CreateRequest,_ string,proposed FrozenExportDocuments)(FrozenExportDocuments,CompiledExportPlan,error){
 s.events=append(s.events,"draft");if s.mutate{proposed.body[0]='!'};if s.fail=="draft"{return FrozenExportDocuments{},CompiledExportPlan{},s.cause};return s.frozen,CompiledExportPlan{},nil
}
func(s *exportCreationAdapters)InstallExportExecution(_ context.Context,_ auth.Identity,_ uuid.UUID,_ CreateRequest,_ string,frozen FrozenExportDocuments)(ExportExecutionInstallation,error){
 s.events=append(s.events,"installation");if s.mutate{frozen.body[0]='!'};if s.fail=="installation"{return ExportExecutionInstallation{},s.cause};return s.installation,nil
}
func TestExportCreationPreparationComposesCommittedDraftAndExecution(t *testing.T){
 for _,mode:=range []string{"valid","mutation","proposal","draft","installation","missing_dependencies","missing_qualifier","materialize","qualification"}{t.Run(mode,func(t *testing.T){
  tid,who,intent,frozen,resolver:=exportExecutionFixture(t);cause:=errors.New("stage unavailable")
  resources:=ExportTaskResources{RAMBytes:2<<30,ScratchBytes:2<<30,OutputBytes:1<<30,MaxInputJSONBytes:128<<20,ContractsSHA:rawSHA([]byte("schemas"))}
  pipeline:=&exportPipelineStub{resolver:resolver};qualifier:=&exportResourceQualifierStub{resources:resources}
  adapters:=&exportCreationAdapters{frozen:frozen,cause:cause,fail:mode,mutate:mode=="mutation",installation:ExportExecutionInstallation{Resources:resources,Execution:ExportExecutionPreparation{Documents:pipeline,Resolver:pipeline,Materializer:pipeline,TransportDocuments:pipeline,Registrar:pipeline},Activation:ExportActivationPreparation{Resolver:pipeline,Resources:qualifier}}}
  p:=ExportCreationPreparation{Proposals:adapters,Drafts:adapters,Installation:adapters}
  switch mode{case "missing_dependencies":p.Drafts=nil;case "missing_qualifier":adapters.installation.Activation.Resources=nil;case "materialize":pipeline.fail="materialize";case "qualification":qualifier.err=cause}
  receipt,err:=p.PrepareExportCreation(context.Background(),who,tid,intent.Request,"pipeline-key")
  if mode=="valid"||mode=="mutation"{if err!=nil||receipt.task.ID!=intent.Identity.Task||len(pipeline.events)==0||qualifier.calls!=1{t.Fatal("creation preparation",err)}}else{if err==nil||receipt.task.ID!=uuid.Nil{t.Fatal("failed stage exposed activation receipt",err)}}
  if (mode=="proposal"||mode=="draft"||mode=="installation"||mode=="qualification")&&!errors.Is(err,cause){t.Fatal("stage cause lost",err)}
  if (mode=="missing_dependencies"||mode=="missing_qualifier"||mode=="draft"||mode=="proposal"||mode=="installation")&&len(pipeline.events)!=0{t.Fatal("failed preflight published documents")}
  if mode=="missing_dependencies"&&len(adapters.events)!=0{t.Fatal("missing dependency performed IO")}
 })}
}
