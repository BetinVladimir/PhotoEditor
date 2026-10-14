package httpapi
import("context";"encoding/json";"net/http/httptest";"strings";"testing";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/interchange";"photoeditor/backend/internal/idempotency";"photoeditor/backend/internal/v2input")
type interchangeCreateStub struct{calls int;mode string}
func(s *interchangeCreateStub)CreateInterchange(_ context.Context,who auth.Identity,tid uuid.UUID,request interchange.CreateRequest,key string)(idempotency.Response,error){
 s.calls++;intent,err:=interchange.FreezeCreateIntent(tid,who,request,key);if err!=nil{return idempotency.Response{},err}
 id:=intent.Identity;ticket:=v2input.JobTicket{RunID:id.Run,WorkflowID:id.Workflow,State:"queued"}
 if s.mode=="wrong-ticket"{ticket.RunID=uuid.New()}
 body,_:=json.Marshal(ticket);resource:=id.Workflow;if s.mode=="wrong-resource"{resource=uuid.New()}
 return idempotency.Response{Status:202,Body:body,ResourceID:&resource,Replayed:s.mode=="replay"},nil
}
func TestCreateInterchangeHTTPValidationAndTicket(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid:=uuid.New()
 request:=interchange.CreateRequest{ProjectID:uuid.New(),Direction:"import",Format:"photoeditor_bundle",Bundle:&v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 raw,_:=json.Marshal(request)
 for _,mode:=range []string{"fresh","replay","wrong-ticket","wrong-resource","missing-key","unknown","duplicate","incompatible"}{t.Run(mode,func(t *testing.T){
  payload:=string(raw);key:="interchange-key";expected:=202
  switch mode{
  case "wrong-ticket","wrong-resource":expected=503
  case "missing-key":key="";expected=422
  case "unknown":payload=payload[:len(payload)-1]+`,"extra":true}`;expected=422
  case "duplicate":payload=payload[:len(payload)-1]+`,"direction":"export"}`;expected=422
  case "incompatible":payload=payload[:len(payload)-1]+`,"snapshot_id":"`+uuid.NewString()+`"}`;expected=422
  }
  stub:=&interchangeCreateStub{mode:mode};r:=httptest.NewRequest("POST","/",strings.NewReader(payload));r.Header.Set("Idempotency-Key",key);w:=httptest.NewRecorder()
  serveCreateInterchange(w,r,who,tid,stub)
  if w.Code!=expected{t.Fatalf("status=%d body=%s",w.Code,w.Body.String())}
  if expected==422&&stub.calls!=0{t.Fatal("invalid request reached service")}
  if mode=="replay"&&w.Header().Get("Idempotency-Replayed")!="true"{t.Fatal("missing replay marker")}
 })}
}
func TestCreateInterchangeRouteRequiresSession(t *testing.T){
 server:=&Server{Auth:&auth.Service{Pepper:[]byte("interchange-create")}};w:=httptest.NewRecorder()
 server.Routes().ServeHTTP(w,httptest.NewRequest("POST","/api/v1/tenants/"+uuid.NewString()+"/interchange-jobs",strings.NewReader(`{}`)))
 if w.Code!=401{t.Fatalf("status=%d",w.Code)}
}
