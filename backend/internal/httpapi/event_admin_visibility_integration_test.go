package httpapi

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "testing"
 "strings"
 "os"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/eventstream"
 "time"
 "github.com/google/uuid"
)

// The private event deliberately carries a visible collection, like Download's
// terminal event. Its collection reference must never widen its audience.
func TestIntegrationEventsAudienceAndRetentionArePrincipalScoped(t *testing.T) {
 h,db,token,tenant:=httpFixture(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 var actor,other,projectID,collectionID uuid.UUID
 if err:=db.QueryRow(ctx,`SELECT owner_user_id FROM photo.tenants WHERE id=$1`,tenant).Scan(&actor);err!=nil{t.Fatal(err)}
 if err:=db.QueryRow(ctx,`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@event.test").Scan(&other);err!=nil{t.Fatal(err)}
 if err:=db.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Event scope') RETURNING id`,tenant).Scan(&projectID);err!=nil{t.Fatal(err)}
 if err:=db.QueryRow(ctx,`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Event scope') RETURNING id`,tenant,projectID).Scan(&collectionID);err!=nil{t.Fatal(err)}
 if _,err:=db.Exec(ctx,`INSERT INTO photo.run_events(tenant_id,cursor,principal_id,collection_id,audience,type,payload) VALUES($1,1,$2,$3,'principal','download_status','{"private":true}'),($1,100,$4,$3,'principal','download_status','{"mine":true}')`,tenant,other,collectionID,actor);err!=nil{t.Fatal(err)}
 get:=func(after string)*httptest.ResponseRecorder{t.Helper();r:=httptest.NewRequest(http.MethodGet,"/api/v1/tenants/"+tenant.String()+"/events?after="+after,nil);r.Header.Set("Authorization","Bearer "+token);r.Header.Set("X-Photo-Client","native");w:=httptest.NewRecorder();h.ServeHTTP(w,r);return w}
 w:=get("0");if w.Code!=200{t.Fatal(w.Code,w.Body.String())}
 var page struct{Items []struct{Cursor string `json:"cursor"`;Payload map[string]bool `json:"payload"`};Next string `json:"next_cursor"`}
 if err:=json.Unmarshal(w.Body.Bytes(),&page);err!=nil{t.Fatal(err)}
 if len(page.Items)!=1||page.Items[0].Cursor!="100"||!page.Items[0].Payload["mine"]||page.Next!="100"{t.Fatal("foreign principal event exposed",w.Body.String())}
 w=get("50");if w.Code!=410{t.Fatal("foreign principal cursor suppressed own retention expiry",w.Code,w.Body.String())}
 for _,cursor:=range []string{"01","%2B1","-0"}{w=get(cursor);if w.Code!=422{t.Fatal("noncanonical cursor accepted",cursor,w.Code)}}
}

func TestIntegrationTransferOwnerRejectsNoncanonicalRevisionWire(t *testing.T) {
 h,_,token,tenant:=httpFixture(t)
 for _,revision:=range []string{"01","+1"," 1","0","9223372036854775808"}{
 body:=`{"target_user_id":"`+uuid.NewString()+`","expected_revision":"`+revision+`"}`
 r:=httptest.NewRequest(http.MethodPost,"/api/v1/tenants/"+tenant.String()+"/transfer-owner",strings.NewReader(body));r.Header.Set("Authorization","Bearer "+token);r.Header.Set("X-Photo-Client","native");r.Header.Set("Content-Type","application/json")
 w:=httptest.NewRecorder();h.ServeHTTP(w,r);if w.Code!=422{t.Fatal(revision,w.Code,w.Body.String())}
 }
}

func TestIntegrationEventMinimumWaitCannotRetainDemotedAdminAudience(t *testing.T) {
 _,admin,_,tenantID:=httpFixture(t);ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
 cfg,err:=pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"));if err!=nil{t.Fatal(err)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
 api,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer api.Close()
 var who auth.Identity
 if err=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@event-admin.test").Scan(&who.UserID,&who.Epoch);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'admin')`,tenantID,who.UserID);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'native',now()+interval '1 hour') RETURNING id`,who.UserID,auth.HashSecret(uuid.NewString()),uuid.New()).Scan(&who.SessionID);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.run_events(tenant_id,cursor,audience,type,payload) VALUES($1,100,'tenant','admin.notice','{"private":true}')`,tenantID);err!=nil{t.Fatal(err)}
 holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(context.Background())
 var pid int;if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil{t.Fatal(err)}
 if _,err=holder.Exec(ctx,`LOCK TABLE photo.run_events IN ACCESS EXCLUSIVE MODE`);err!=nil{t.Fatal(err)}
 type result struct{page eventstream.Page;err error};done:=make(chan result,1)
 go func(){p,e:=(eventstream.Service{DB:api}).List(ctx,who,tenantID,"50",50);done<-result{p,e}}()
 tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop()
 waiting:=false;for !waiting{select{case got:=<-done:t.Fatal("minimum wait not reached",got.err);case <-ctx.Done():t.Fatal(ctx.Err());case <-tick.C:if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,pid).Scan(&waiting);err!=nil{t.Fatal(err)}}}
 if _,err=admin.Exec(ctx,`UPDATE photo.memberships SET role='editor',revision=revision+1 WHERE tenant_id=$1 AND user_id=$2`,tenantID,who.UserID);err!=nil{t.Fatal(err)}
 if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
 got:=<-done;if domain.AsError(got.err).Code!="NOT_FOUND"||len(got.page.Items)!=0{t.Fatal("stale Admin audience or retention response exposed",got.page,got.err)}
}
