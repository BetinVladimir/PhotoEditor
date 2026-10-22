package httpapi

import (
 "context"
 "net/http/httptest"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2portrait"
)

// Real authentication/tenant middleware; explicit submission stub isolates HTTP
// forwarding. SQL admission/qualification is covered by separate fixtures.
type relightReplayHTTPStub struct{calls int;replay bool;key string;who auth.Identity;err error}
func (q *relightReplayHTTPStub) record(ctx context.Context,who auth.Identity,key string){q.calls++;q.replay=v2portrait.IsRelightReplayOnly(ctx);q.key=key;q.who=who}
func (q *relightReplayHTTPStub) SubmitProjectKeyed(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,key string,intent v2portrait.RelightDraftIntent)([]byte,error){
 q.record(ctx,who,key);if q.err!=nil{return nil,q.err};run:=uuid.New();workflow,_:=v2input.WorkflowID(run)
 return canonical.Marshal(map[string]any{"schema_version":1,"run_id":run,"workflow_id":workflow,"actor_id":who.UserID,"auth_session_id":who.SessionID,"state":"queued"})
}
func (q *relightReplayHTTPStub) SubmitProjectAnalysis(ctx context.Context,who auth.Identity,tenant,project uuid.UUID,key string,intent v2portrait.RelightAnalysisIntent)(v2portrait.RelightAnalysisReceipt,error){
 q.record(ctx,who,key);if q.err!=nil{return v2portrait.RelightAnalysisReceipt{},q.err};run:=uuid.New();workflow,_:=v2input.WorkflowID(run);task,_:=v2input.TaskID(workflow,intent.Operation,0)
 return v2portrait.RelightAnalysisReceipt{SchemaVersion:1,Run:run,Workflow:workflow,Task:task,Actor:who.UserID,Session:who.SessionID,Operation:intent.Operation,State:"queued"},nil
}
func TestIntegrationRelightReplayHTTP(t *testing.T){
 q:=&relightReplayHTTPStub{}
 handler,db,token,tenant:=httpFixtureWithServerOptions(t,nil,nil,nil,func(s *Server){s.RelightSubmit=q;s.RelightAnalysis=q})
 id:=uuid.New();ref:=map[string]any{"artifact_id":id,"sha256":strings.Repeat("a",64)}
 base:=map[string]any{"asset_version_id":id,"source":ref,"canvas_hash":strings.Repeat("b",64)}
 analysis,_:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_id":id,"base":base,"operation":"depth.predict"})
 relight,_:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_id":id,"ordinal":0,"subject_kind":"manual","params":map[string]any{"schema_version":1,"base":base,"strength":.5,"depth":ref,"subject":ref,"protected":ref,"calibration":ref,"lights":[]any{map[string]any{"x":.5,"y":.5,"radius":.3,"ev":0}}}})
 for _,lane:=range []struct{path string;body []byte}{{"relight-intents",relight},{"relight-analyses",analysis}}{
  path:="/api/v1/tenants/"+tenant.String()+"/projects/"+id.String()+"/"+lane.path
  for _,tc:=range []struct{name string;headers []string;status int;err error;called,replay bool}{
   {name:"normal",status:202,called:true},{name:"replay",headers:[]string{"true"},status:202,called:true,replay:true},
   {name:"invalid",headers:[]string{"false"},status:422},{name:"duplicate",headers:[]string{"true","true"},status:422},
   {name:"absent",headers:[]string{"true"},status:410,err:v2input.ErrExpired,called:true,replay:true},
   {name:"denied",headers:[]string{"true"},status:404,err:domain.NewError("NOT_FOUND",404),called:true,replay:true},
  }{t.Run(lane.path+"/"+tc.name,func(t *testing.T){
   before:=q.calls;q.err=tc.err
   r:=httptest.NewRequest("POST",path,strings.NewReader(string(lane.body)));r.Header.Set("Content-Type","application/json");r.Header.Set("X-Photo-Client","native");r.Header.Set("Authorization","Bearer "+token);r.Header.Set("Idempotency-Key","original-key")
   for _,value:=range tc.headers{r.Header.Add("Relight-Replay-Only",value)}
   w:=httptest.NewRecorder();handler.ServeHTTP(w,r)
   if w.Code!=tc.status{t.Fatal(w.Code,w.Body.String())}
   delta:=0;if tc.called{delta=1};if q.calls!=before+delta{t.Fatal("invalid header reached submitter")}
   if tc.called&&(q.replay!=tc.replay||q.key!="original-key"||q.who.UserID==uuid.Nil||q.who.SessionID==uuid.Nil){t.Fatal("replay/identity/key not forwarded")}
  })}
 }
 // The same replay header must not bypass a revoked authenticated session.
 if _,err:=db.Exec(context.Background(),"UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1",q.who.SessionID);err!=nil{t.Fatal(err)}
 before:=q.calls
 revoked:=httptest.NewRequest("POST","/api/v1/tenants/"+tenant.String()+"/projects/"+id.String()+"/relight-analyses",strings.NewReader(string(analysis)))
 revoked.Header.Set("Content-Type","application/json");revoked.Header.Set("X-Photo-Client","native");revoked.Header.Set("Authorization","Bearer "+token);revoked.Header.Set("Idempotency-Key","original-key");revoked.Header.Set("Relight-Replay-Only","true")
 response:=httptest.NewRecorder();handler.ServeHTTP(response,revoked)
 if response.Code!=401||q.calls!=before{t.Fatal("revoked session reached relight submission",response.Code)}
}
