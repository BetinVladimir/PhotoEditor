package httpapi
import("context";"encoding/json";"net/http/httptest";"strings";"testing";"github.com/go-chi/chi/v5";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/idempotency")
type interchangeCloseStub struct{calls int;mode string}
func(s *interchangeCloseStub)CloseInterchangeReview(_ context.Context,_ auth.Identity,_,id uuid.UUID,_ string,_ []byte)(idempotency.Response,error){
 s.calls++;workflow:=id;if s.mode=="foreign"{workflow=uuid.New()}
 body,_:=json.Marshal(map[string]any{"run_id":uuid.New(),"workflow_id":workflow,"revision":"2","state":"completed","outputs":[]any{},"error_code":nil})
 return idempotency.Response{Status:200,Body:body,ResourceID:&id,Replayed:s.mode=="replay"},nil
}
func TestInterchangeCloseHTTPContract(t *testing.T){
 for _,mode:=range []string{"valid","replay","foreign","missing-key","duplicate","unknown"}{t.Run(mode,func(t *testing.T){
  payload:=`{"expected_revision":"1","report_sha":"`+strings.Repeat("a",64)+`"}`;key:="close-1";expected:=200
  switch mode{case "foreign":expected=503;case "missing-key":key="";expected=422;case "duplicate":payload=strings.Replace(payload,`"expected_revision":"1"`,`"expected_revision":"1","expected_revision":"2"`,1);expected=422;case "unknown":payload=payload[:len(payload)-1]+`,"extra":true}`;expected=422}
  request:=httptest.NewRequest("POST","/",strings.NewReader(payload));request.Header.Set("Idempotency-Key",key)
  route:=chi.NewRouteContext();route.URLParams.Add("id",uuid.NewString());request=request.WithContext(context.WithValue(request.Context(),chi.RouteCtxKey,route))
  stub:=&interchangeCloseStub{mode:mode};response:=httptest.NewRecorder()
  serveCloseInterchangeReview(response,request,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),stub)
  if response.Code!=expected{t.Fatalf("status %d: %s",response.Code,response.Body.String())}
  if expected==422&&stub.calls!=0{t.Fatal("invalid input reached service")}
  if mode=="replay"&&response.Header().Get("Idempotency-Replayed")!="true"{t.Fatal("missing replay header")}
 })}
}
func TestInterchangeCloseRouteRequiresSession(t *testing.T){
 server:=&Server{Auth:&auth.Service{Pepper:[]byte("close-review")}};response:=httptest.NewRecorder()
 server.Routes().ServeHTTP(response,httptest.NewRequest("POST","/api/v1/tenants/"+uuid.NewString()+"/interchange-jobs/"+uuid.NewString()+"/close-review",strings.NewReader(`{}`)))
 if response.Code!=401{t.Fatalf("status %d",response.Code)}
}
