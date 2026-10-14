package v2optical

import (
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type negativeRegistrationDB struct{tx *negativeRegistrationTx;begins int}
func(d *negativeRegistrationDB)Begin(context.Context)(pgx.Tx,error){
 d.begins++
 if d.begins==2&&d.tx.mode=="qualification_changed"{d.tx.negativeLockTx.mode="qualification_stale"}
 if d.begins==2&&d.tx.mode=="commit_registration"{d.tx.saveMode="commit_error"}
 return d.tx,nil
}
type negativeRegistrationTx struct{
 negativeDraftStoreTx
 mode string
 wire []byte
 expectedDraft string
 readCalls,registerCalls int
}
func(t *negativeRegistrationTx)QueryRow(ctx context.Context,q string,args ...any)pgx.Row{
 if q!="SELECT photo.read_v29_negative_published_transport($1,$2,$3,$4,$5::jsonb)"&&q!="SELECT photo.register_v29_negative_transport($1,$2,$3,$4::jsonb,$5::jsonb,$6)"{return t.negativeDraftStoreTx.QueryRow(ctx,q,args...)}
 t.calls=append(t.calls,q)
 return negativeLockRow(func(dest ...any)error{
  var proof struct{Draft string `json:"draft_jcs"`;Objects []NegativeQualificationObject `json:"qualification_objects"`}
  proofIndex:=4;if len(args)==6{proofIndex=3}
  if args[0]!=t.tenant||json.Unmarshal([]byte(args[proofIndex].(string)),&proof)!=nil||proof.Draft!=t.expectedDraft||len(proof.Objects)!=2{return errors.New("registration lost original owner/qualification")}
  if len(args)==5{
   t.readCalls++
   if t.mode=="missing"{*dest[0].(*[]byte)=nil}else{*dest[0].(*[]byte)=append([]byte(nil),t.wire...)}
  }else{
   t.registerCalls++
   if t.commits!=1{return errors.New("registration before metadata read committed")}
   var transport negativeTransportTestWire;var manifest v2input.Manifest
   if json.Unmarshal([]byte(args[4].(string)),&transport)!=nil||json.Unmarshal([]byte(args[5].(string)),&manifest)!=nil{return errors.New("invalid registration contracts")}
   var original negativeTransportTestWire;json.Unmarshal(t.wire,&original)
   digest,err:=canonical.Hash(manifest);if err!=nil||digest!=original.Manifest.SHA256||transport.Manifest!=original.Manifest||manifest.EntriesSHA!=original.SHA||manifest.EntryCount!=original.Count{return errors.New("registration differs from published transport")}
   if t.mode=="query_error"{return errors.New("registration dependency failure")}
   *dest[0].(*bool)=t.mode!="refused"
  }
  return nil
 })
}
func TestNegativeRegistrationRestoresThenRepeatsAuthorityBeforeAtomicInputSeal(t *testing.T){
 for _,mode:=range []string{"valid","missing","refused","qualification_changed","query_error","commit_registration"}{
  t.Run(mode,func(t *testing.T){
   draft,located,who,_:=negativeParamsFixture(t);snapshot,_:=draft.Snapshot();wire,params:=negativeTransportRecoveryFixture(t,draft,located);raw,_:=canonical.Marshal(wire)
   tx:=&negativeRegistrationTx{negativeDraftStoreTx:negativeDraftStoreTx{negativeSourceScopeTx:negativeSourceScopeTx{negativeLockTx:negativeLockTx{tenant:snapshot.Owner.TenantID}}},mode:mode,wire:raw,expectedDraft:draft.raw}
   db:=&negativeRegistrationDB{tx:tx}
   graph,err:=RegisterNegativeInputs(context.Background(),db,"photos",who,snapshot.Owner.TenantID,draft,located,params)
   if mode=="valid"{
    if err!=nil||graph.ID!=snapshot.Owner.TaskID||tx.commits!=2||tx.registerCalls!=1{t.Fatal("valid closure not registered",err)}
   }else if err==nil||graph.ID!=uuid.Nil{t.Fatal("failed registration returned complete graph",mode)}
   if (mode=="missing"||mode=="qualification_changed")&&tx.registerCalls!=0{t.Fatal("registration reached without fresh metadata/qualification")}
   if tx.readCalls!=1||tx.rollbacks!=db.begins{t.Fatal("recovery or transaction cleanup missing")}
   if mode=="refused"&&!errors.Is(err,v2input.ErrExpired){t.Fatal("registration refusal lost",err)}
  })
 }
}
