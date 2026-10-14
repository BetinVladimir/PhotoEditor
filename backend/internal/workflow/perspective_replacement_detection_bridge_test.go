package workflow
import (
 "bytes"
 "context"
 "net/http"
 "net/http/httptest"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)
func TestReplacementDetectorBridgeRejectsUnauthenticatedAndForeignPackets(t *testing.T){
 d,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(d);if err!=nil{t.Fatal(err)}
 db:=&detectionDocumentDB{}
 source:=ReplacementDetectionDocuments{DB:db,Bucket:"photos",Snapshot:uuid.New(),Plan:PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}}
 bridge,token,err:=NewReplacementDetectionAdoptionBridge(context.Background(),source);if err!=nil{t.Fatal(err)}
 if len(token)!=64{t.Fatal("missing job token")}
 packet:=replacementDetectionBridgeRequest{SchemaVersion:1,Tenant:d.Tenant,Run:d.Run,Task:d.Task,Attempt:1,Fence:1,Result:uuid.New()}
 for _,scenario:=range []struct{name,path,authorization string;foreign,malformed bool;status int}{
  {name:"missing token",path:"/evidence",status:http.StatusUnauthorized},
  {name:"wrong token",path:"/evidence",authorization:"Bearer wrong",status:http.StatusUnauthorized},
  {name:"foreign scope",path:"/evidence",authorization:"Bearer "+token,foreign:true,status:http.StatusBadRequest},
  {name:"unknown route",path:"/unknown",authorization:"Bearer "+token,status:http.StatusBadRequest},
  {name:"noncanonical document",path:"/evidence",authorization:"Bearer "+token,malformed:true,status:http.StatusBadRequest},
 }{t.Run(scenario.name,func(t *testing.T){
  value:=packet;if scenario.foreign{value.Task=uuid.New()}
  body,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)};if scenario.malformed{body=append(body,' ')}
  request:=httptest.NewRequest(http.MethodPost,scenario.path,bytes.NewReader(body));request.Header.Set("Authorization",scenario.authorization);request.Header.Set("Content-Type","application/json")
  response:=httptest.NewRecorder();bridge.serveHTTP(response,request)
  if response.Code!=scenario.status{t.Fatal("unexpected bridge response",response.Code)}
 })}
 if db.calls!=0{t.Fatal("invalid bridge request reached database")}
}
