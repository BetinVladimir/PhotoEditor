package v2optical

import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

// Protocol-only fixture; whole replacement physical/SQL lifecycle remains separate.
func TestReplacementReceiptBindsDecisionAndOriginalTask(t *testing.T){
 draft,located,_,_:=negativeParamsFixture(t)
 wire,published:=negativeTransportRecoveryFixture(t,draft,located)
 qualified,err:=located.Qualification();if err!=nil{t.Fatal(err)}
 pin,err:=qualified.Pin();if err!=nil{t.Fatal(err)}
 ordinary,err:=draft.Snapshot();if err!=nil{t.Fatal(err)}
 descriptor,err:=draft.Descriptor(context.Background(),qualified,published);if err!=nil{t.Fatal(err)}
 digest,err:=canonical.Hash(json.RawMessage(descriptor));if err!=nil{t.Fatal(err)}
 transport,err:=canonical.Marshal(wire);if err!=nil{t.Fatal(err)}
 identity:=NegativeReplacementPublicationIdentity{SchemaVersion:2,Review:uuid.New(),OriginalTask:uuid.New(),OriginalRun:uuid.New(),DecisionHash:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 identitySHA,err:=canonical.Hash(identity);if err!=nil{t.Fatal(err)}
 expected:=negativeReplacementDraftSnapshot{SchemaVersion:2,Owner:ordinary.Owner,Identity:identity,IdentitySHA:identitySHA}
 receipt:=NegativeReplacementReceipt{SchemaVersion:2,Run:ordinary.Owner.RunID,Workflow:ordinary.Owner.WorkflowID,Task:ordinary.Owner.TaskID,State:"queued",Actor:ordinary.Owner.ActorID,Session:ordinary.Owner.AuthSessionID,Operation:"optical.negative",DescriptorSHA:digest,Qualification:pin,Transport:transport,ReplacementIdentitySHA:identitySHA,Review:identity.Review,DecisionHash:identity.DecisionHash,OriginalTask:identity.OriginalTask,OriginalRun:identity.OriginalRun}
 for _,mode:=range []string{"valid","schema","identity","review","decision","original_task","original_run","run","session","descriptor","transport","unknown"}{
  changed:=receipt;changed.Transport=append(json.RawMessage(nil),receipt.Transport...)
  switch mode{
  case "schema":changed.SchemaVersion=1
  case "identity":changed.ReplacementIdentitySHA=digest
  case "review":changed.Review=uuid.New()
  case "decision":changed.DecisionHash="changed"
  case "original_task":changed.OriginalTask=uuid.New()
  case "original_run":changed.OriginalRun=uuid.New()
  case "run":changed.Run=uuid.New()
  case "session":changed.Session=uuid.New()
  case "descriptor":changed.DescriptorSHA=wire.SHA
  case "transport":changed.Transport=json.RawMessage(`{}`)
  }
  body,err:=canonical.Marshal(changed);if err!=nil{t.Fatal(err)}
  if mode=="unknown"{var object map[string]any;if err=json.Unmarshal(body,&object);err!=nil{t.Fatal(err)};object["extra"]=true;body,err=canonical.Marshal(object);if err!=nil{t.Fatal(err)}}
  got,err:=parseNegativeReplacementReceipt(context.Background(),expected,descriptor,pin,body)
  if mode!="valid"{if err==nil{t.Fatal("forged replacement receipt accepted",mode)};continue}
  if err!=nil||got.OriginalTask!=identity.OriginalTask{t.Fatal("protocol receipt refused",err)}
  got.Transport[0]='[';body[0]='['
  again,err:=canonical.Marshal(receipt);if err!=nil{t.Fatal(err)}
  if _,err=parseNegativeReplacementReceipt(context.Background(),expected,descriptor,pin,again);err!=nil{t.Fatal("mutable receipt storage",err)}
 }
}
