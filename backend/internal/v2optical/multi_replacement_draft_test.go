package v2optical
import (
 "bytes"
 "context"
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func TestMultiReplacementPacketPreservesOrderedSourcesAndIdentity(t *testing.T){
 for _,operation:=range []string{"optical.panorama","optical.focus_stack"}{t.Run(operation,func(t *testing.T){
  ctx:=context.Background();pin,locks,report,calibration,params,originals:=multiQualifiedFixture(t,operation)
  qualified,err:=VerifyMultiQualificationEvidence(operation,pin,locks,report,calibration,params,originals);if err!=nil{t.Fatal(err)}
  capacity,err:=MultiResources(operation,params,originals);if err!=nil{t.Fatal(err)}
  plan,err:=CompileMultiTaskPlan(ctx,operation,params,originals,qualified,capacity);if err!=nil{t.Fatal(err)}
  marker:=[]byte(`{"schema_version":2}`);markerSHA,_:=canonical.Hash(json.RawMessage(marker))
  identity:=NegativeReplacementPublicationIdentity{SchemaVersion:2,Tenant:uuid.New(),Project:uuid.New(),Review:uuid.New(),DecisionHash:strings.Repeat("d",64),OriginalTask:uuid.New(),OriginalRun:uuid.New(),Operation:operation,ParamsSHA:plan.ParamsSHA,MarkerSHA:markerSHA}
  namespace:=uuid.NewSHA1(identity.Tenant,[]byte("V29.NegativeReplacement/v2:"+identity.Project.String()+":"+identity.Review.String()+":"+identity.DecisionHash))
  identity.Run=uuid.NewSHA1(namespace,[]byte("run:"+identity.OriginalRun.String()+":"+identity.OriginalTask.String()));identity.Task=uuid.NewSHA1(namespace,[]byte("task:"+identity.OriginalTask.String()));identity.Workflow,_=v2input.WorkflowID(identity.Run);identity.ParamsDocument=uuid.NewSHA1(identity.Task,[]byte("params"));digest,_:=canonical.Hash(identity)
  snapshot,actor,session:=uuid.New(),uuid.New(),uuid.New()
  compile:=func(i NegativeReplacementPublicationIdentity,m []byte,sources [][]byte)(MultiReplacementDraft,error){return compileMultiReplacementDraftPacket(ctx,i,digest,snapshot,actor,session,m,plan,qualified,sources)}
  draft,err:=compile(identity,marker,originals);if err!=nil{t.Fatal(err)}
  again,err:=compile(identity,marker,originals);if err!=nil||!bytes.Equal(draft.Bytes(),again.Bytes()){t.Fatal("unstable packet",err)}
  var saved multiReplacementDraftSnapshot;if json.Unmarshal(draft.Bytes(),&saved)!=nil||saved.Identity!=identity||saved.Draft.Task!=identity.Task||len(saved.Draft.SourcesJCS)!=len(originals){t.Fatal("identity/order lost")}
  for i,raw:=range originals{if saved.Draft.SourcesJCS[i]!=string(raw){t.Fatal("source order changed")}}
  published:=v2input.Ref{ArtifactID:identity.ParamsDocument,SHA256:identity.ParamsSHA}
  descriptor,err:=draft.Descriptor(ctx,qualified,published);if err!=nil{t.Fatal(err)}
  var decoded struct{Task v2input.DraftTask `json:"task"`;Entries []v2input.Entry `json:"entries"`}
  if json.Unmarshal(descriptor,&decoded)!=nil||decoded.Task.ID!=identity.Task||decoded.Task.Operation!=operation||decoded.Task.InputDigest!=saved.InputDigest||len(decoded.Entries)<3{t.Fatal("descriptor identity/inventory lost")}
  if _,err=(MultiDraft{raw:string(mustMultiReplacementSnapshot(t,saved.Draft))}).Descriptor(ctx,qualified,published);err==nil{t.Fatal("ordinary multi path accepted replacement task")}
  for _,alter:=range []func(*multiReplacementDraftSnapshot){
   func(p *multiReplacementDraftSnapshot){p.InputDigest=strings.Repeat("f",64)},
   func(p *multiReplacementDraftSnapshot){p.Draft.ReservedBytes++},
   func(p *multiReplacementDraftSnapshot){p.Draft.Task=uuid.New()},
   func(p *multiReplacementDraftSnapshot){p.Draft.SourcesJCS[0]="{}"},
  }{
   var mutated multiReplacementDraftSnapshot;json.Unmarshal(draft.Bytes(),&mutated);alter(&mutated);raw,_:=canonical.Marshal(mutated)
   if _,err=(MultiReplacementDraft{raw:string(raw)}).Descriptor(ctx,qualified,published);err==nil{t.Fatal("mutated descriptor accepted")}
  }
  exposed:=draft.Bytes();exposed[0]='x';if draft.Bytes()[0]!='{'{t.Fatal("mutable packet")}
  bad:=identity;bad.Task=uuid.New();if _,err=compile(bad,marker,originals);err==nil{t.Fatal("foreign task accepted")}
  if _,err=compile(identity,[]byte(`{}`),originals);err==nil{t.Fatal("foreign marker accepted")}
  changed:=append([][]byte(nil),originals...);changed[0]=[]byte(`{}`);if _,err=compile(identity,marker,changed);err==nil{t.Fatal("foreign source accepted")}
 })}
}
func TestMultiReplacementDraftRequiresPhysicalAuthorityAndCancellation(t *testing.T){
 var step PreparedMappedReplacementStep
 if _,err:=step.CompileMultiReplacementDraft(context.Background(),uuid.New(),MultiQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,MultiResourceEstimate{});err==nil{t.Fatal("missing mapped authority accepted")}
 for _,raw:=range [][]byte{nil,[]byte(`{}`),[]byte(`{"schema_version":2,"draft":{}}`)}{if _,err:=step.RestoreMultiReplacementDraft(context.Background(),raw,uuid.New(),MultiQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,MultiResourceEstimate{});err==nil{t.Fatal("unscoped recovery accepted")}}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=step.CompileMultiReplacementDraft(ctx,uuid.New(),MultiQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,MultiResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
}

func mustMultiReplacementSnapshot(t *testing.T,s MultiDraftSnapshot)[]byte{
 t.Helper();raw,err:=canonical.Marshal(s);if err!=nil{t.Fatal(err)};return raw
}
func TestMultiReplacementLockRequiresTransaction(t *testing.T){
 var prepared PreparedNegativeReplacementInputs
 if _,err:=prepared.LockMultiReplacementDraft(context.Background(),nil,nil,nil,nil,nil,[]byte(`{}`),uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{});err==nil{t.Fatal("missing transaction accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=prepared.LockMultiReplacementDraft(ctx,nil,nil,nil,nil,nil,[]byte(`{}`),uuid.New(),LocatedMultiQualification{},v2qualification.DeploymentLocks{},MultiResourceEstimate{});err!=context.Canceled{t.Fatal(err)}
}
