package gateway

import (
 "context"
 "errors"
 "strings"
 "testing"
 "time"
 "github.com/jackc/pgx/v5"
)

type fenceInstallationDB struct { called bool; deadline time.Time; permitted bool; begin,complete,result string; err error }
type fenceInstallationRow struct { db *fenceInstallationDB }
func(d *fenceInstallationDB)QueryRow(ctx context.Context,query string,_ ...any)pgx.Row{
 d.called=true;d.deadline,_=ctx.Deadline()
 if !strings.Contains(query,"pg_catalog.pg_proc")||!strings.Contains(query,"has_function_privilege"){panic("installation omitted installed body/privilege inspection")}
 return fenceInstallationRow{d}
}
func(r fenceInstallationRow)Scan(dest ...any)error{if r.db.err!=nil{return r.db.err};*dest[0].(*string)=r.db.begin;*dest[1].(*string)=r.db.complete;*dest[2].(*string)=r.db.result;*dest[3].(*bool)=r.db.permitted;return nil}
func fenceInstallationFixture()*fenceInstallationDB{return &fenceInstallationDB{permitted:true,
 begin:"FOR UPDATE; -- 02536: photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate) old.state='ready' FOR UPDATE OF x; -- 02536: photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate) q.expires_at<=clock_timestamp() UPDATE photo.quota_reservations SET bytes=bytes-p_bytes",
 complete:"FOR UPDATE; -- 02536: photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate) IF u.id IS NULL IF u.state='ready' INSERT INTO photo.artifacts",
 result:"OUTPUT_NOT_VERIFIED -- 02535: t.lease_until<=clock_timestamp() slot.lease_until<=clock_timestamp() -- 02537: photo.execution_source_live(p_task) INSERT INTO photo.task_results("}}
func TestOutputInstallationRefusesOldMissingAndUnauthorizedBodies(t *testing.T){
 for _,kind:=range []string{"old_begin","old_complete","old_result","quota_expiry_missing","privilege_missing","query_error"}{t.Run(kind,func(t *testing.T){d:=fenceInstallationFixture();switch kind{case "old_begin":d.begin="legacy";case "old_complete":d.complete="legacy";case "old_result":d.result="legacy";case "quota_expiry_missing":d.begin=strings.ReplaceAll(d.begin,"q.expires_at<=clock_timestamp()","");case "privilege_missing":d.permitted=false;case "query_error":d.err=errors.New("catalog unavailable")};if RequireOutputFences(context.Background(),d)==nil{t.Fatal("unsafe installation accepted")}})}
}
func TestOutputInstallationBoundedAndCanceled(t *testing.T){
 d:=fenceInstallationFixture();before:=time.Now();if err:=RequireOutputFences(context.Background(),d);err!=nil{t.Fatal(err)}
 if !d.called||d.deadline.Before(before)||d.deadline.After(time.Now().Add(5*time.Second)){t.Fatal("missing bounded catalog deadline")}
 ctx,cancel:=context.WithCancel(context.Background());cancel();d.called=false
 if err:=RequireOutputFences(ctx,d);err!=context.Canceled||d.called{t.Fatal("canceled startup inspected database",err)}
}
