package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

type negativeStageDB struct{tx *negativeStageTx;begins int}
func(d *negativeStageDB)Begin(ctx context.Context)(pgx.Tx,error){
 deadline,ok:=ctx.Deadline();if !ok||time.Until(deadline)>15*time.Second{return nil,errors.New("unbounded staging transaction")}
 d.begins++;return d.tx,nil
}
type negativeStageTx struct{
 negativeDraftStoreTx
 stageMode string
 descriptor,proof string
}
func(t *negativeStageTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row{
 if q!="SELECT photo.stage_v29_negative_task($1,$2,$3,$4::jsonb)"{return t.negativeDraftStoreTx.QueryRow(ctx,q,args...)}
 t.calls=append(t.calls,q)
 return negativeLockRow(func(dest ...any)error{
  if len(args)!=4||args[0]!=t.tenant{return errors.New("wrong staging scope")}
  t.descriptor=args[2].(string);t.proof=args[3].(string)
  if t.stageMode=="query_error"{return errors.New("stage dependency failure")}
  *dest[0].(*bool)=t.stageMode!="refused"
  return nil
 })
}

func TestNegativeDescriptorBindsCompleteInventoryAndPublishedParams(t *testing.T){
 draft,located,_,_:=negativeParamsFixture(t);snapshot,_:=draft.Snapshot();qualified,err:=located.Qualification();if err!=nil{t.Fatal(err)}
 published:=v2input.Ref{ArtifactID:snapshot.ParamsDocument,SHA256:snapshot.ParamsSHA}
 raw,err:=draft.Descriptor(context.Background(),qualified,published);if err!=nil{t.Fatal(err)}
 var descriptor struct{SchemaVersion int `json:"schema_version"`;Task v2input.DraftTask `json:"task"`;Entries []v2input.Entry `json:"entries"`;ReservedBytes int64 `json:"reserved_bytes"`}
 if json.Unmarshal(raw,&descriptor)!=nil{t.Fatal("invalid descriptor")}
 digest,err:=negativeDraftInputDigest(snapshot.Owner.TaskID,descriptor.Entries);if err!=nil{t.Fatal(err)}
 if descriptor.SchemaVersion!=1||descriptor.Task.ID!=snapshot.Owner.TaskID||descriptor.Task.NodeKey!="optical.negative:0"||descriptor.Task.Operation!="optical.negative"||descriptor.Task.OperationVersion!="1"||descriptor.Task.ResourceClass!="cpu"||descriptor.Task.ParamsArtifactID!=published.ArtifactID||descriptor.Task.ParamsSHA!=published.SHA256||descriptor.Task.InputDigest!=digest||digest!=snapshot.InputDigest||descriptor.ReservedBytes!=snapshot.ReservedBytes{t.Fatal("descriptor lost saved identity/inventory/resources")}
 contracts:=map[string]int{}
 for i,entry:=range descriptor.Entries{
  contracts[entry.Contract]++
  if i>0&&descriptor.Entries[i-1].Ref.ArtifactID.String()>=entry.Ref.ArtifactID.String(){t.Fatal("inventory not strictly sorted")}
 }
 if contracts["Params"]!=1||contracts["WorkingImage"]!=1||contracts["NegativeCalibration"]!=1||contracts["QualificationReport"]!=1||contracts["NpyArray"]<1{t.Fatal("complete typed inventory missing",contracts)}
 stable:=bytes.Clone(raw);raw[0]='['
 again,err:=draft.Descriptor(context.Background(),qualified,published);if err!=nil||!bytes.Equal(stable,again){t.Fatal("descriptor caller mutation changed saved state",err)}
 published.ArtifactID=uuid.New()
 if _,err:=draft.Descriptor(context.Background(),qualified,published);err==nil{t.Fatal("foreign published Params accepted")}
}

func TestNegativeStageLocksLiveAuthorityAndCommitsOnlyAcceptedShell(t *testing.T){
 draft,located,who,_:=negativeParamsFixture(t);snapshot,_:=draft.Snapshot()
 published:=v2input.Ref{ArtifactID:snapshot.ParamsDocument,SHA256:snapshot.ParamsSHA}
 for _,mode:=range []string{"ok","child_stale","qualification_stale","refused","query_error","commit_error","foreign_actor","foreign_params","cancelled"}{
  t.Run(mode,func(t *testing.T){
   tx:=&negativeStageTx{negativeDraftStoreTx:negativeDraftStoreTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:snapshot.Owner.TenantID,mode:mode}},saveMode:mode},stageMode:mode}
   db:=&negativeStageDB{tx:tx};actor:=who;params:=published
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   if mode=="foreign_actor"{actor=auth.Identity{UserID:uuid.New(),SessionID:who.SessionID}}
   if mode=="foreign_params"{params.ArtifactID=uuid.New()}
   if mode=="cancelled"{cancel()}
   err:=StageNegativeTask(ctx,db,"photos",actor,snapshot.Owner.TenantID,draft,located,params)
   if mode=="ok"{
    if err!=nil||tx.commits!=1||tx.descriptor==""||tx.proof==""{t.Fatal("valid shell not staged",err)}
    var proof struct{Draft string `json:"draft_jcs"`;Objects []NegativeQualificationObject `json:"qualification_objects"`}
    if json.Unmarshal([]byte(tx.proof),&proof)!=nil||proof.Draft!=draft.raw||len(proof.Objects)!=2{t.Fatal("exact stored draft/locator proof lost")}
   }else if err==nil{t.Fatal("invalid staging accepted",mode)}
   if mode=="foreign_actor"||mode=="foreign_params"||mode=="cancelled"{
    if db.begins!=0{t.Fatal("invalid local scope began transaction")}
   }else{
    if tx.rollbacks!=1{t.Fatal("staging transaction not cleaned up")}
    if mode!="ok"&&mode!="commit_error"&&tx.commits!=0{t.Fatal("failed staging committed")}
    if (mode=="child_stale"||mode=="qualification_stale")&&tx.descriptor!=""{t.Fatal("staging ran before live authority")}
   }
   if mode=="refused"&&!errors.Is(err,v2input.ErrExpired){t.Fatal("stale refusal lost",err)}
  })
 }
}
