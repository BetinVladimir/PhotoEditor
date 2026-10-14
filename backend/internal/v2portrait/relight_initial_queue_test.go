package v2portrait

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
)

func TestRelightInitialQueueConfigReplay(t *testing.T) {
 project,session:=uuid.New(),uuid.New()
 request,pin:=[]byte(`{"schema_version":1}`),[]byte(`{"policy_sha":"pin"}`)
 for _,queue:=range []string{"interactive",""} {
  raw,err:=compileRelightInitialConfig(project,session,request,pin,queue);if err!=nil{t.Fatal(err)}
  again,err:=compileRelightInitialConfig(project,session,request,pin,queue);if err!=nil||string(raw)!=string(again){t.Fatal("replay changed",err)}
  var value map[string]json.RawMessage
  if err:=json.Unmarshal(raw,&value);err!=nil{t.Fatal(err)}
  field,present:=value["queue_class"]
  if present!=(queue!="")||(present&&string(field)!=`"interactive"`){t.Fatal("queue changed")}
 }
 for _,queue:=range []string{"batch","training","capture"} {
  if _,err:=compileRelightInitialConfig(project,session,request,pin,queue);err==nil{t.Fatal("foreign queue accepted")}
 }
}
