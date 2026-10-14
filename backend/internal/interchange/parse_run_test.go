package interchange

import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/domain"
 "testing"
 "github.com/google/uuid"
)
func TestBeginParseRunScopesAndBindsSnapshotConfig(t *testing.T){
 tid,who,request,evidence,limits:=pendingFixture();plan,err:=CompileParsePlan(tid,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
 tx:=&proofTx{accepted:true}
 if err:=plan.BeginParseRun(context.Background(),tx,who,tid,request,"key");err!=nil{t.Fatal(err)}
 if len(tx.args)!=8||tx.args[0]!=tid||tx.args[1]!=who.UserID||tx.args[2]!=who.SessionID||tx.args[3]!="key"{t.Fatal("run scope changed")}
 var config map[string]any;json.Unmarshal([]byte(tx.args[5].(string)),&config)
 if config["queue_class"]!="batch"||config["workflow_type"]!="interchange"||config["snapshot_id"]!=plan.Identity().Snapshot.String(){t.Fatal("wrong run config")}
 tx.args=nil
 if err:=plan.BeginParseRun(context.Background(),tx,who,uuid.New(),request,"key");err==nil||tx.args!=nil{t.Fatal("foreign run queried")}
 tx.accepted=false
 if err:=plan.BeginParseRun(context.Background(),tx,who,tid,request,"key");err==nil{t.Fatal("SQL refusal ignored")}
 tx.runtimeUnavailable=true
 if err:=plan.BeginParseRun(context.Background(),tx,who,tid,request,"key");domain.AsError(err)==nil||domain.AsError(err).Code!="INTERCHANGE_UNAVAILABLE"{t.Fatal("missing scheduler runtime was not reported as unavailable",err)}
}

func TestParseQueuePendingHistoricalReplay(t *testing.T){
 tid,who,request,evidence,limits:=pendingFixture()
 raw,err:=EncodeParsePending(tid,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
 var saved map[string]json.RawMessage
 if err:=json.Unmarshal(raw,&saved);err!=nil{t.Fatal(err)}
 for _,legacy:=range []bool{false,true}{
  if legacy{delete(saved,"queue_class")}
  body,err:=json.Marshal(saved);if err!=nil{t.Fatal(err)}
  plan,err:=DecodeParsePending(tid,who,request,"key",body);if err!=nil{t.Fatal(err)}
  tx:=&proofTx{accepted:true}
  if err:=plan.BeginParseRun(context.Background(),tx,who,tid,request,"key");err!=nil{t.Fatal(err)}
  first:=tx.args[5].(string)
  var config map[string]json.RawMessage
  if err:=json.Unmarshal([]byte(first),&config);err!=nil{t.Fatal(err)}
  _,present:=config["queue_class"];if present==legacy{t.Fatal("saved queue choice changed")}
  if err:=plan.BeginParseRun(context.Background(),tx,who,tid,request,"key");err!=nil{t.Fatal(err)}
  if tx.args[5].(string)!=first{t.Fatal("replay config changed")}
 }
 for _,value:=range []string{`"interactive"`,`"training"`,`null`,`""`}{
  saved["queue_class"]=json.RawMessage(value)
  body,err:=json.Marshal(saved);if err!=nil{t.Fatal(err)}
  if _,err:=DecodeParsePending(tid,who,request,"key",body);err==nil{t.Fatal("foreign queue accepted",value)}
 }
}
