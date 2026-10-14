package v2optical

import (
 "context"
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestNegativeReceiptBindsOriginalDraftAndTransport(t *testing.T) {
 draft,located,_,_:=negativeParamsFixture(t)
 wire,published:=negativeTransportRecoveryFixture(t,draft,located)
 qualified,_:=located.Qualification();pin,_:=qualified.Pin()
 snapshot,_:=draft.Snapshot();descriptor,err:=draft.Descriptor(context.Background(),qualified,published);if err!=nil { t.Fatal(err) }
 digest,err:=canonical.Hash(json.RawMessage(descriptor));if err!=nil { t.Fatal(err) }
 transport,err:=canonical.Marshal(wire);if err!=nil { t.Fatal(err) }
 receipt:=NegativeReceipt{SchemaVersion:1,Run:snapshot.Owner.RunID,Workflow:snapshot.Owner.WorkflowID,Task:snapshot.Owner.TaskID,State:"queued",Actor:snapshot.Owner.ActorID,Session:snapshot.Owner.AuthSessionID,Operation:"optical.negative",DescriptorSHA:digest,Qualification:pin,Transport:transport}
 for _,mode:=range []string{"valid","run","workflow","task","actor","session","state","operation","descriptor","qualification","transport","unknown"} {
  t.Run(mode,func(t *testing.T){
   changed:=receipt;changed.Transport=append(json.RawMessage(nil),transport...)
   switch mode {
   case "run":changed.Run=uuid.New()
   case "workflow":changed.Workflow=uuid.New()
   case "task":changed.Task=uuid.New()
   case "actor":changed.Actor=uuid.New()
   case "session":changed.Session=uuid.New()
   case "state":changed.State="succeeded"
   case "operation":changed.Operation="optical.perspective"
   case "descriptor":changed.DescriptorSHA=wire.SHA
   case "qualification":changed.Qualification=NegativeQualificationPin{}
   case "transport":changed.Transport=json.RawMessage(`{}`)
   }
   body,err:=canonical.Marshal(changed);if err!=nil { t.Fatal(err) }
   if mode=="unknown" { var m map[string]any;json.Unmarshal(body,&m);m["extra"]=true;body,_=canonical.Marshal(m) }
   got,err:=parseNegativeReceipt(context.Background(),draft,located,published,body)
   if mode!="valid" { if err==nil { t.Fatal("forged receipt accepted") };return }
   if err!=nil || got.Task!=receipt.Task { t.Fatal("valid receipt refused",err) }
   got.Transport[0]='[';body[0]='['
   again,_:=canonical.Marshal(receipt)
   if _,err:=parseNegativeReceipt(context.Background(),draft,located,published,again);err!=nil { t.Fatal("caller mutation changed receipt",err) }
  })
 }
}
