package httpapi
import("context";"encoding/json";"net/http/httptest";"strings";"testing";"github.com/go-chi/chi/v5";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/idempotency";"photoeditor/backend/internal/interchange")
type interchangeDecideStub struct{calls int;mode string}
func(s *interchangeDecideStub)DecideInterchangeReview(ctx context.Context,who auth.Identity,tid,id uuid.UUID,key string,raw []byte)(idempotency.Response,error){
 s.calls++;intent,err:=interchange.FreezeImportReviewIntent(ctx,tid,id,who,key,"decide",raw);if err!=nil{return idempotency.Response{},err};batch:=intent.OwnerID();var child any
 if s.mode=="child"{child=map[string]any{"run_id":uuid.New()}}
 workflow:=id;if s.mode=="foreign"{workflow=uuid.New()}
 body,_:=json.Marshal(map[string]any{"workflow_id":workflow,"revision":"2","remaining":0,"child":child,"batch_id":batch})
 return idempotency.Response{Status:200,Body:body,ResourceID:&batch,Replayed:s.mode=="replay"},nil
}
func TestImportDecideHTTPValidatesBatchAndNullChild(t *testing.T){
 for _,mode:=range []string{"valid","replay","child","foreign","duplicate","missing-key"}{t.Run(mode,func(t *testing.T){
  entry:=uuid.NewString();payload:=`{"expected_revision":"1","report_sha":"`+strings.Repeat("a",64)+`","decisions":[{"entry_id":"`+entry+`","decision":"reject"}]}`;key:="decide-1";expected:=200
  switch mode{case "child","foreign":expected=503;case "missing-key":key="";expected=422;case "duplicate":payload=strings.Replace(payload,`"decision":"reject"`,`"decision":"reject","decision":"approve"`,1);expected=422}
  request:=httptest.NewRequest("POST","/",strings.NewReader(payload));request.Header.Set("Idempotency-Key",key);route:=chi.NewRouteContext();route.URLParams.Add("id",uuid.NewString());request=request.WithContext(context.WithValue(request.Context(),chi.RouteCtxKey,route))
  stub:=&interchangeDecideStub{mode:mode};response:=httptest.NewRecorder();serveDecideInterchangeReview(response,request,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),stub)
  if response.Code!=expected{t.Fatalf("status=%d: %s",response.Code,response.Body.String())};if expected==422&&stub.calls!=0{t.Fatal("invalid input reached service")};if mode=="replay"&&response.Header().Get("Idempotency-Replayed")!="true"{t.Fatal("missing replay header")}
 })}
}
func TestImportDecideRouteRequiresSession(t *testing.T){server:=&Server{Auth:&auth.Service{Pepper:[]byte("decide")}};response:=httptest.NewRecorder();server.Routes().ServeHTTP(response,httptest.NewRequest("POST","/api/v1/tenants/"+uuid.NewString()+"/interchange-jobs/"+uuid.NewString()+"/decisions",strings.NewReader(`{}`)));if response.Code!=401{t.Fatalf("status=%d",response.Code)}}
