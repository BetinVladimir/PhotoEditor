package interchange

import (
 "context"
 "encoding/json"
 "testing"
)
func TestExportDraftConfigPinsExistingSnapshot(t *testing.T){
 tenant,who,intent,compiled,ref,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,ref,raw);if err!=nil{t.Fatal(err)}
 body,err:=exportDraftConfig(context.Background(),intent,frozen);if err!=nil{t.Fatal(err)}
 var config map[string]any;if err:=json.Unmarshal(body,&config);err!=nil{t.Fatal(err)}
 if config["queue_class"]!="batch"||config["snapshot_id"]!=intent.Identity.Snapshot.String()||config["request_sha"]!=intent.RequestSHA||config["workflow_type"]!="interchange"{t.Fatal("draft scope mismatch")}
 if err:=frozen.BeginExportRun(context.Background(),nil,who,tenant,intent.Request,"export-key");err==nil{t.Fatal("nil transaction accepted")}
 bad:=frozen;bad.body=[]byte(`{}`)
 if _,err:=exportDraftConfig(context.Background(),intent,bad);err==nil{t.Fatal("corrupt frozen payload accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=exportDraftConfig(ctx,intent,frozen);err==nil{t.Fatal("cancelled draft compilation accepted")}
}

func TestExportQueueHistoricalFrozenReplay(t *testing.T){
 _,_,intent,compiled,ref,raw:=exportLedgerFixture(t)
 ctx:=context.Background()
 fresh,err:=FreezeExportDocuments(ctx,intent,compiled,ref,raw);if err!=nil{t.Fatal(err)}
 var payload map[string]json.RawMessage
 if err:=json.Unmarshal(fresh.Body(),&payload);err!=nil{t.Fatal(err)}
 delete(payload,"queue_class")
 old,err:=json.Marshal(payload);if err!=nil{t.Fatal(err)}
 historical,_,err:=DecodeFrozenExportDocuments(ctx,intent,old);if err!=nil{t.Fatal(err)}
 first,err:=exportDraftConfig(ctx,intent,historical);if err!=nil{t.Fatal(err)}
 var config map[string]json.RawMessage
 if err:=json.Unmarshal(first,&config);err!=nil{t.Fatal(err)}
 if _,ok:=config["queue_class"];ok{t.Fatal("historical queue omission changed")}
 recovered,_,err:=DecodeFrozenExportDocuments(ctx,intent,historical.Body());if err!=nil{t.Fatal(err)}
 again,err:=exportDraftConfig(ctx,intent,recovered);if err!=nil||string(again)!=string(first){t.Fatal("historical config replay changed",err)}
 for _,value:=range []string{`"interactive"`,`"training"`,`null`,`""`}{
  payload["queue_class"]=json.RawMessage(value)
  invalid,err:=json.Marshal(payload);if err!=nil{t.Fatal(err)}
  if _,_,err:=DecodeFrozenExportDocuments(ctx,intent,invalid);err==nil{t.Fatal("foreign queue accepted",value)}
 }
}
