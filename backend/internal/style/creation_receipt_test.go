package style

import (
 "encoding/json"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/idempotency"
)

func TestStyleCreationReceiptBindsOriginalScopeAndBody(t *testing.T){
 tid,id,pid:=uuid.New(),uuid.New(),uuid.New();req:=Create{ProjectID:pid,Name:" Wedding ",Description:"original"}
 item:=Style{ID:id,TenantID:tid,ProjectID:pid,Name:"Wedding",Description:"original",Revision:"1",CreatedAt:time.Now()}
 body,e:=canonical.Marshal(item);if e!=nil{t.Fatal(e)};valid:=idempotency.Response{Status:201,ResourceID:&id,Body:body}
 if _,e=readCreatedStyleReceipt(valid,tid,req);e!=nil{t.Fatal(e)}
 for _,change:=range []func(*Style){func(v *Style){v.ProjectID=uuid.New()},func(v *Style){v.Revision="2"},func(v *Style){v.Archived=true},func(v *Style){v.Name="Different"}}{
  changed:=item;change(&changed);raw,e:=canonical.Marshal(changed);if e!=nil{t.Fatal(e)};receipt:=valid;receipt.Body=raw;if _,e=readCreatedStyleReceipt(receipt,tid,req);e==nil{t.Fatal("accepted changed receipt")}
 }
 var extra map[string]any;if e=json.Unmarshal(body,&extra);e!=nil{t.Fatal(e)};extra["unexpected"]=true;raw,_:=json.Marshal(extra);valid.Body=raw
 if _,e=readCreatedStyleReceipt(valid,tid,req);e==nil{t.Fatal("accepted unknown receipt fields")}
}
