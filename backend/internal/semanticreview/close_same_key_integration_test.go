package semanticreview

import (
    "context"
    "encoding/json"
    "errors"
    "os"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
    "photoeditor/backend/internal/canonical"
    "photoeditor/backend/internal/idempotency"
    "photoeditor/backend/internal/domain"
)

// The Python harness creates and drops a private database. These metadata
// scenarios exercise actual Use/SQL/replay, not physical object publication.
func TestIntegrationCloseSameKeyWait(t *testing.T) {
    dsn,path:=os.Getenv("PHOTO_TEST_DATABASE_URL"),os.Getenv("PHOTO_TEST_CLOSE_FIXTURE")
    if os.Getenv("PHOTO_TEST_DISPOSABLE")!="1" || dsn=="" || path=="" { t.Skip("disposable close fixture required") }
    mode:=os.Getenv("PHOTO_TEST_CLOSE_MODE")
    if mode!="success" && mode!="revoked" && mode!="competing" && mode!="changed_request" && mode!="rollback" && mode!="rollback_competing" { t.Fatal("invalid close schedule") }
    var f struct {
        Tenant,Intent,Actor,Session,Parent,Artifact uuid.UUID
        Revision int64
        Key,Request,Body string
        Documents json.RawMessage
    }
    raw,err:=os.ReadFile(path);if err!=nil { t.Fatal(err) }
    if err=json.Unmarshal(raw,&f);err!=nil { t.Fatal(err) }
    ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
    connect:=func()*pgx.Conn{c,e:=pgx.Connect(ctx,dsn);if e!=nil { t.Fatal(e) };t.Cleanup(func(){c.Close(context.Background())});return c}
    holder,reader,observer:=connect(),connect(),connect()
    for _,c:=range []*pgx.Conn{holder,reader} { if _,err=c.Exec(ctx,"SET ROLE photo_api");err!=nil { t.Fatal(err) } }
    requestSHA,err:=canonical.Hash(json.RawMessage(f.Request));if err!=nil { t.Fatal(err) }
    batch:=decisionDocumentOwner(f.Tenant,f.Parent,f.Actor,requestSHA)
    workflow:=uuid.Nil
    if err=observer.QueryRow(ctx,"SELECT workflow_id FROM photo.semantic_intents WHERE tenant_id=$1 AND id=$2",f.Tenant,f.Intent).Scan(&workflow);err!=nil { t.Fatal(err) }
    var callbacks atomic.Int32
    use:=func(tx pgx.Tx,key,request string)(idempotency.Response,error){
        if _,e:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",f.Tenant.String());e!=nil { return idempotency.Response{},e }
        return idempotency.Use(ctx,tx,idempotency.Scope{Tenant:f.Tenant,Principal:f.Actor,PrincipalType:"user",Route:CloseIdempotencyRoute,Key:key},json.RawMessage(request),time.Now().Add(time.Hour),func()(idempotency.Response,error){
            callbacks.Add(1)
            var body json.RawMessage
            e:=tx.QueryRow(ctx,"SELECT photo.commit_semantic_review_close($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)",f.Tenant,f.Intent,f.Actor,f.Session,key,f.Revision,request,f.Artifact,f.Body,f.Documents).Scan(&body)
            return idempotency.Response{Status:200,Body:body,ResourceID:&batch},e
        })
    }
    tx,err:=holder.Begin(ctx);if err!=nil { t.Fatal(err) };defer tx.Rollback(context.Background())
    first,err:=use(tx,f.Key,f.Request);if err!=nil || first.Replayed { t.Fatal(first,err) }
    contenderKey,contenderRequest:=f.Key,f.Request
    if mode=="competing" || mode=="rollback_competing" { contenderKey=f.Key+"-other" }
    if mode=="changed_request" {
        var request map[string]any
        if err=json.Unmarshal([]byte(f.Request),&request);err!=nil { t.Fatal(err) }
        request["request"].(map[string]any)["plan_sha"]=strings.Repeat("0",64)
        changed,e:=canonical.Marshal(request);if e!=nil { t.Fatal(e) }
        contenderRequest=string(changed)
    }
    type outcome struct{ response idempotency.Response; err error }
    done:=make(chan outcome,1);afterUse,proceed:=make(chan struct{}),make(chan struct{})
    workerDone:=make(chan struct{})
    defer func(){ cancel(); select {case <-workerDone:case <-time.After(3*time.Second):t.Error("close waiter did not terminate")} }()
    go func(){
        defer close(workerDone)
        rt,e:=reader.Begin(ctx);if e!=nil { done<-outcome{err:e};return };defer rt.Rollback(context.Background())
        response,e:=use(rt,contenderKey,contenderRequest)
        if e==nil {
            close(afterUse)
            select {case <-proceed:case <-ctx.Done():done<-outcome{err:ctx.Err()};return}
            if response.Replayed {
                response,e=verifyCloseCommitReplay(ctx,response,f.Parent,workflow,batch,func(ctx context.Context)(ReplayResult,error){return ReadCloseReplay(ctx,rt,f.Tenant,f.Intent,f.Actor,f.Session,f.Key,requestSHA)})
            }
        }
        if e==nil { e=rt.Commit(ctx) }
        done<-outcome{response,e}
    }()
    deadline:=time.NewTimer(5*time.Second);defer deadline.Stop()
    ticker:=time.NewTicker(10*time.Millisecond);defer ticker.Stop()
    blocked:=false
    for !blocked {
        if err=observer.QueryRow(ctx,"SELECT $1::integer=ANY(pg_blocking_pids($2::integer))",holder.PgConn().PID(),reader.PgConn().PID()).Scan(&blocked);err!=nil { t.Fatal(err) }
        if blocked { break }
        select {case o:=<-done:t.Fatal("reader did not wait",o.err);case <-deadline.C:t.Fatal("authority wait not observed");case <-ticker.C:}
    }
    if mode=="rollback" || mode=="rollback_competing" { err=tx.Rollback(ctx) } else { err=tx.Commit(ctx) }
    if err!=nil { t.Fatal(err) }
    var second outcome
    if mode=="success" || mode=="revoked" || mode=="rollback" || mode=="rollback_competing" {
        select {case <-afterUse:case o:=<-done:t.Fatal(o.err);case <-ctx.Done():t.Fatal(ctx.Err())}
        if mode=="revoked" {
            if _,err=observer.Exec(ctx,"UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1",f.Session);err!=nil { t.Fatal(err) }
        }
        close(proceed)
    }
    select {case second=<-done:case <-ctx.Done():t.Fatal(ctx.Err())}
    if mode=="success" {
        if second.err!=nil || !second.response.Replayed || string(second.response.Body)!=string(first.Body) || second.response.ResourceID==nil || *second.response.ResourceID!=batch { t.Fatal(second.response,second.err) }
    } else if mode=="rollback" || mode=="rollback_competing" {
        if second.err!=nil || second.response.Replayed || string(second.response.Body)!=string(first.Body) || second.response.ResourceID==nil || *second.response.ResourceID!=batch { t.Fatal("rollback waiter did not perform fresh close",second.response,second.err) }
    } else if mode=="changed_request" {
        if second.err==nil || domain.AsError(second.err).Code!="IDEMPOTENCY_CONFLICT" || len(second.response.Body)!=0 { t.Fatal("changed request accepted",second.err) }
    } else {
        expected:="42501";if mode=="competing" { expected="40001" }
        var pgerr *pgconn.PgError
        if !errors.As(second.err,&pgerr) || pgerr.Code!=expected || len(second.response.Body)!=0 { t.Fatal("unsafe contender outcome",second.err) }
    }
    expectedCallbacks:=int32(1);if mode=="competing" || mode=="rollback" || mode=="rollback_competing" { expectedCallbacks=2 }
    if callbacks.Load()!=expectedCallbacks { t.Fatal("unexpected mutation callback count",callbacks.Load()) }
    select {case <-workerDone:case <-ctx.Done():t.Fatal(ctx.Err())}
    for _,query:=range []string{
        "SELECT count(*) FROM photo.v2_review_batches WHERE tenant_id=$1 AND action='close'",
        "SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND type='v27.review.closed'",
        "SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND routing_key='v27.review.closed'",
        "SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND route='SemanticIntent.CloseReview' AND response_status=200",
    } {
        var count int
        if err=observer.QueryRow(ctx,query,f.Tenant).Scan(&count);err!=nil || count!=1 { t.Fatal("duplicate/missing close effects",count,err) }
    }
    var parentState string
    var parentRevision int64
    if err=observer.QueryRow(ctx,"SELECT status,revision FROM photo.runs WHERE tenant_id=$1 AND id=$2",f.Tenant,f.Parent).Scan(&parentState,&parentRevision);err!=nil || parentState!="completed" || parentRevision!=f.Revision+1 { t.Fatal("parent closed more than once",parentState,parentRevision,err) }
    var receipts int
    if err=observer.QueryRow(ctx,"SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND route='SemanticIntent.CloseReview'",f.Tenant).Scan(&receipts);err!=nil || receipts!=1 { t.Fatal("losing receipt was not rolled back",receipts,err) }
    t.Log("V27_CLOSE_SAME_KEY_WAIT_OK")
}
