package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type importExecutionStageFunc func(context.Context,uuid.UUID,ImportPreparedDocuments,v2input.DraftTask,[]byte)error
func(f importExecutionStageFunc)Prepare(c context.Context,t uuid.UUID,d ImportPreparedDocuments,k v2input.DraftTask,p []byte)error{return f(c,t,d,k,p)}
type importQualificationStageFunc func(context.Context,uuid.UUID,ImportPreparedDocuments,v2input.DraftTask)(QualifiedImportActivation,error)
func(f importQualificationStageFunc)Prepare(c context.Context,t uuid.UUID,d ImportPreparedDocuments,k v2input.DraftTask)(QualifiedImportActivation,error){return f(c,t,d,k)}
type importActivationStageFunc func(context.Context,QualifiedImportActivation)error
func(f importActivationStageFunc)ActivateImport(c context.Context,q QualifiedImportActivation)error{return f(c,q)}
func TestImportCoordinatorStagesAndRefusals(t *testing.T){
 for _,mode:=range []string{"valid","execution","qualification","activation","substitution","ownership","cancel"}{t.Run(mode,func(t *testing.T){
  docs,task,_,parsed:=measuredImportFixtureWithParsed(t);tenant:=uuid.New()
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  calls:="";sentinel:=errors.New("stage refusal")
  coordinator:=ImportExecutionCoordinator{
   Execution:importExecutionStageFunc(func(_ context.Context,_ uuid.UUID,d ImportPreparedDocuments,k v2input.DraftTask,p []byte)error{
    calls+="E";if mode=="execution"{return sentinel};if mode=="cancel"{cancel()}
    if mode=="ownership"{d.plan.Body[0]='!';d.evidence.Body[0]='!';k.ExecutionProfile[0]='!';p[0]='!'};return nil
   }),
   Qualification:importQualificationStageFunc(func(_ context.Context,scope uuid.UUID,d ImportPreparedDocuments,k v2input.DraftTask)(QualifiedImportActivation,error){
    calls+="Q";if mode=="qualification"{return QualifiedImportActivation{},sentinel}
    if d.plan.Body[0]!='{'||d.evidence.Body[0]!='{'||k.ExecutionProfile[0]!='{'{t.Fatal("callback mutated retained inputs")}
    if mode=="substitution"{k.ParamsSHA=rawSHA([]byte("other"))}
    return QualifiedImportActivation{tenant:scope,documents:d,task:k,digest:k.InputDigest},nil
   }),
   Activation:importActivationStageFunc(func(_ context.Context,q QualifiedImportActivation)error{calls+="A";if mode=="activation"{return sentinel};return nil}),
  }
  err:=coordinator.Execute(ctx,tenant,docs,task,parsed)
  success:=mode=="valid"||mode=="ownership"
  if (err==nil)!=success{t.Fatal("unexpected coordinator result",err)}
  expected:="EQA";switch mode{case "execution","cancel":expected="E";case "qualification","substitution":expected="EQ"}
  if calls!=expected{t.Fatal("stage ordering",calls,expected)}
  if mode=="execution"||mode=="qualification"||mode=="activation"{if !errors.Is(err,sentinel){t.Fatal("original error lost",err)}}
  if mode=="cancel"&&err!=context.Canceled{t.Fatal("cancellation lost",err)}
 })}
}


type importActivationRecoveryFunc func(context.Context,uuid.UUID,ImportPreparedDocuments,v2input.DraftTask,[]byte)(bool,error)
func(f importActivationRecoveryFunc)RecoverImportActivation(c context.Context,t uuid.UUID,d ImportPreparedDocuments,k v2input.DraftTask,p []byte)(bool,error){return f(c,t,d,k,p)}
func TestImportCoordinatorRecoversOnlyConfirmedAdmission(t *testing.T){
 for _,mode:=range []string{"already_admitted","concurrent_admission","ambiguous_commit","unconfirmed_commit","recovery_error"}{t.Run(mode,func(t *testing.T){
  docs,task,_,parsed:=measuredImportFixtureWithParsed(t);tenant:=uuid.New();calls:="";reads:=0;sentinel:=errors.New("commit outcome unknown")
  c:=ImportExecutionCoordinator{
   Recovery:importActivationRecoveryFunc(func(_ context.Context,scope uuid.UUID,d ImportPreparedDocuments,k v2input.DraftTask,p []byte)(bool,error){
    calls+="R";reads++;if scope!=tenant||d.plan.Ref!=docs.plan.Ref||k.InputDigest!=task.InputDigest||rawSHA(p)!=rawSHA(parsed){t.Fatal("recovery scope changed")}
    if mode=="recovery_error"{return false,sentinel}
    return mode=="already_admitted"||reads==2&&(mode=="concurrent_admission"||mode=="ambiguous_commit"),nil
   }),
   Execution:importExecutionStageFunc(func(context.Context,uuid.UUID,ImportPreparedDocuments,v2input.DraftTask,[]byte)error{calls+="E";if mode=="concurrent_admission"{return sentinel};return nil}),
   Qualification:importQualificationStageFunc(func(_ context.Context,scope uuid.UUID,d ImportPreparedDocuments,k v2input.DraftTask)(QualifiedImportActivation,error){calls+="Q";return QualifiedImportActivation{tenant:scope,documents:d,task:k,digest:k.InputDigest},nil}),
   Activation:importActivationStageFunc(func(context.Context,QualifiedImportActivation)error{calls+="A";return sentinel}),
  }
  err:=c.Execute(context.Background(),tenant,docs,task,parsed)
  expected:="REQAR";switch mode{case "already_admitted","recovery_error":expected="R";case "concurrent_admission":expected="RER"}
  if calls!=expected{t.Fatal("recovery ordering",calls,expected)}
  success:=mode!="unconfirmed_commit"&&mode!="recovery_error"
  if (err==nil)!=success{t.Fatal("unconfirmed outcome accepted",err)}
  if !success&&!errors.Is(err,sentinel){t.Fatal("original failure lost",err)}
 })}
}
