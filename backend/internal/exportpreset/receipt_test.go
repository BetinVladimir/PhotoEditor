package exportpreset

import (
 "encoding/json"
 "testing"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/idempotency"
)

func TestPresetReceiptBindsCompleteCreateAndRejectsCorruption(t *testing.T){
 request:=validCreate();id:=uuid.New();hash,err:=canonical.Hash(request.Config);if err!=nil{t.Fatal(err)}
 value:=Preset{ID:id,Name:request.Name,Revision:"1",Config:request.Config,ConfigHash:hash,CreatedAt:time.Now().UTC()}
 body,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
 original:=idempotency.Response{Status:201,ResourceID:&id,Body:body}
 if err=validateCreatedPresetReceipt(original,request);err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"status","resource","unknown","revision","name","hash","time"}{t.Run(mode,func(t *testing.T){
  receipt:=original;var doc map[string]any;if err=json.Unmarshal(body,&doc);err!=nil{t.Fatal(err)}
  switch mode{case "status":receipt.Status=200;case "resource":other:=uuid.New();receipt.ResourceID=&other;case "unknown":doc["extra"]=true;case "revision":doc["revision"]="2";case "name":doc["name"]="Other";case "hash":doc["config_hash"]="bad";case "time":doc["created_at"]="0001-01-01T00:00:00Z"}
  receipt.Body,err=json.Marshal(doc);if err!=nil{t.Fatal(err)}
  if err=validateCreatedPresetReceipt(receipt,request);err==nil{t.Fatal("corrupt creation accepted",mode)}
 })}
}
