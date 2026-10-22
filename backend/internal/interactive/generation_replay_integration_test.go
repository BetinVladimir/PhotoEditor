package interactive

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"
 "bytes"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
)

// Isolated fixture must seed an authenticated source and committed generation
// receipt, then change only the draft revision or remove qualified workers.
// This exercises real Generate/Use replay, not a new physical GPU execution.
func TestIntegrationGenerationReplayAfterMutableAdmissionChanges(t *testing.T){
 if os.Getenv("PHOTO_TEST_DISPOSABLE")!="1" {t.Skip("disposable generation fixtures required")}
 raw:=os.Getenv("PHOTO_TEST_GENERATION_REPLAY")
 if raw=="" {t.Skip("isolated generation replay fixtures required")}
 var fixtures []struct{Tenant,Actor,Session uuid.UUID;Key,Mode string;Request GenerateRequest;Expected json.RawMessage}
 if json.Unmarshal([]byte(raw),&fixtures)!=nil || len(fixtures)!=2 {t.Fatal("two isolated fixtures required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 cfg,err:=pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"));if err!=nil {t.Fatal(err)}
 cfg.ConnConfig.RuntimeParams["role"]="photo_api"
 db,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil {t.Fatal(err)};defer db.Close()
 seen:=map[string]bool{}
 for _,f:=range fixtures {t.Run(f.Mode,func(t *testing.T){
  if seen[f.Mode] {t.Fatal("duplicate mode")};seen[f.Mode]=true
  who:=auth.Identity{UserID:f.Actor,SessionID:f.Session}
  err:=tenant.WithTenant(ctx,db,who,f.Tenant,domain.Viewer,func(tx pgx.Tx,_ domain.Role)error{
   switch f.Mode {
   case "draft_changed":
    var revision int64;if err:=tx.QueryRow(ctx,"SELECT revision FROM photo.editor_drafts WHERE tenant_id=$1 AND asset_version_id=$2 AND user_id=$3",f.Tenant,f.Request.AssetVersionID,f.Actor).Scan(&revision);err!=nil{return err};if revision==f.Request.DraftRevision {t.Fatal("draft did not change")}
   case "runtime_unavailable":
    var projectID uuid.UUID;if err:=tx.QueryRow(ctx,`SELECT a.project_id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE v.tenant_id=$1 AND v.id=$2`,f.Tenant,f.Request.AssetVersionID).Scan(&projectID);err!=nil{return err}
    if _,_,err:=runtime(ctx,tx,f.Tenant,projectID,who,"image.generate");err==nil || domain.AsError(err).Code!="INTERACTIVE_CAPABILITY_UNAVAILABLE" {t.Fatal("fixture runtime refusal not confirmed",err)}
   default:t.Fatal("invalid mode")
   };return nil
  });if err!=nil {t.Fatal(err)}
  response,err:=(Service{DB:db}).Generate(ctx,who,f.Tenant,f.Key,f.Request)
  expected,e:=canonical.Bytes(f.Expected)
  if err!=nil || e!=nil || !response.Replayed || response.Status!=202 || !bytes.Equal(response.Body,expected) {t.Fatal("committed receipt replay failed",err,response.Status,response.Replayed)}
  replay,err:=(Service{DB:db}).ReplayGeneration(ctx,who,f.Tenant,f.Key,f.Request)
  if err!=nil || !replay.Replayed || !bytes.Equal(replay.Body,expected) {t.Fatal("replay-only lost committed receipt",err)}
  absentKey:=uuid.NewString()
  if _,err=(Service{DB:db}).ReplayGeneration(ctx,who,f.Tenant,absentKey,f.Request);err==nil || domain.AsError(err).Code!="GENERATION_REPLAY_PENDING" {t.Fatal("replay-only admitted new work",err)}
  if err=tenant.WithTenant(ctx,db,who,f.Tenant,domain.Viewer,func(tx pgx.Tx,_ domain.Role)error{
   var count int
   if e:=tx.QueryRow(ctx,"SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Generation.CreateCandidate' AND key=$3",f.Tenant,f.Actor,absentKey).Scan(&count);e!=nil{return e}
   if count!=0 {t.Fatal("replay-only refusal left a pending receipt")}
   if e:=tx.QueryRow(ctx,"SELECT count(*) FROM photo.interactive_tasks WHERE tenant_id=$1",f.Tenant).Scan(&count);e!=nil{return e}
   if count!=0 {t.Fatal("replay-only refusal admitted tasks")}
   return nil
  });err!=nil {t.Fatal(err)}
  existing,err:=(Service{DB:db}).AbandonGeneration(ctx,who,f.Tenant,f.Key,f.Request)
  if err!=nil || existing.Status!=202 || !existing.Replayed || !bytes.Equal(existing.Body,expected) {t.Fatal("abandon discarded an accepted run",err)}
  abandonedKey:=uuid.NewString()
  tombstone,err:=(Service{DB:db}).AbandonGeneration(ctx,who,f.Tenant,abandonedKey,f.Request)
  if err!=nil || tombstone.Status!=200 || tombstone.Replayed || tombstone.ResourceID!=nil {t.Fatal("abandon did not serialize absent admission",err)}
  expectedTombstone,_:=canonical.Marshal(map[string]string{"status":"abandoned","key":abandonedKey})
  if !bytes.Equal(tombstone.Body,expectedTombstone) {t.Fatal("invalid abandonment receipt")}
  again,err:=(Service{DB:db}).AbandonGeneration(ctx,who,f.Tenant,abandonedKey,f.Request)
  if err!=nil || !again.Replayed || !bytes.Equal(again.Body,tombstone.Body) {t.Fatal("abandon receipt retry lost",err)}
  for _,submit:=range []func(context.Context,auth.Identity,uuid.UUID,string,GenerateRequest)(idempotency.Response,error){(Service{DB:db}).Generate,(Service{DB:db}).ReplayGeneration} {
   response,e:=submit(ctx,who,f.Tenant,abandonedKey,f.Request)
   if e==nil || domain.AsError(e).Code!="GENERATION_REQUEST_ABANDONED" || len(response.Body)!=0 {t.Fatal("late generation bypassed abandonment",e)}
  }
  changed:=f.Request;changed.VariantCount=3;if changed.VariantCount==f.Request.VariantCount {changed.VariantCount=2}
  if _,e:=(Service{DB:db}).AbandonGeneration(ctx,who,f.Tenant,abandonedKey,changed);e==nil || domain.AsError(e).Code!="IDEMPOTENCY_CONFLICT" {t.Fatal("changed abandonment body accepted",e)}
  if e:=tenant.WithTenant(ctx,db,who,f.Tenant,domain.Viewer,func(tx pgx.Tx,_ domain.Role)error{
   var retained bool
   if e:=tx.QueryRow(ctx,"SELECT expires_at>clock_timestamp()+interval '100 years' FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Generation.CreateCandidate' AND key=$3",f.Tenant,f.Actor,abandonedKey).Scan(&retained);e!=nil{return e}
   if !retained {t.Fatal("abandonment tombstone expires while old journals may exist")}
   var count int
   if e:=tx.QueryRow(ctx,"SELECT count(*) FROM photo.interactive_tasks WHERE tenant_id=$1",f.Tenant).Scan(&count);e!=nil{return e};if count!=0 {t.Fatal("abandonment admitted generation tasks")};return nil
  });e!=nil {t.Fatal(e)}
  if _,err=(Service{DB:db}).Generate(ctx,who,f.Tenant,f.Key,changed);err==nil || domain.AsError(err).Code!="IDEMPOTENCY_CONFLICT" {t.Fatal("changed request replay accepted",err)}
  if _,err=(Service{DB:db}).Generate(ctx,who,f.Tenant,uuid.NewString(),f.Request);err==nil {t.Fatal("new admission ignored mutable refusal")} else {expectedCode:="REVISION_CONFLICT";if f.Mode=="runtime_unavailable" {expectedCode="INTERACTIVE_CAPABILITY_UNAVAILABLE"};if domain.AsError(err).Code!=expectedCode {t.Fatal("unexpected admission refusal",err)}}
 })}
 if !seen["draft_changed"]||!seen["runtime_unavailable"] {t.Fatal("missing replay refusal mode")}
 if !t.Failed() {t.Log("GENERATION_REPLAY_MUTABLE_AUTHORITY_OK")}
}

// The harness supplies a fresh private database for each authority-loss mode.
// Observe real PostgreSQL contention before changing authority: no sleeps are
// used as evidence that the service has completed its original preflight.
func TestIntegrationGenerationReplayReauthorizesAfterWait(t *testing.T) {
 if os.Getenv("PHOTO_TEST_DISPOSABLE") != "1" { t.Skip("disposable generation fixture required") }
 var fixtures []struct { Tenant,Actor,Session uuid.UUID; Key string; Request GenerateRequest }
 if err := json.Unmarshal([]byte(os.Getenv("PHOTO_TEST_GENERATION_REPLAY")), &fixtures); err != nil || len(fixtures) != 2 { t.Fatal("two isolated fixtures required", err) }
 f := fixtures[0]
 mode := os.Getenv("PHOTO_TEST_GENERATION_AUTHORITY_MODE")
 statements := map[string]string{
  "revoked": "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1",
  "expired": "UPDATE photo.sessions SET expires_at=clock_timestamp() WHERE id=$1",
  "tenant_disabled": "UPDATE photo.tenants SET status='suspended' WHERE id=$1",
  "role_downgraded": "UPDATE photo.memberships SET role='viewer' WHERE tenant_id=$1 AND user_id=$2",
  "project_archived": "UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=(SELECT a.project_id FROM photo.assets a JOIN photo.asset_versions v ON v.tenant_id=a.tenant_id AND v.asset_id=a.id WHERE v.tenant_id=$1 AND v.id=$2)",
  "source_deleted": "UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE tenant_id=$1 AND id=(SELECT asset_id FROM photo.asset_versions WHERE tenant_id=$1 AND id=$2)",
 }
 statement, ok := statements[mode]; if !ok { t.Fatal("invalid authority schedule") }
 ctx,cancel := context.WithTimeout(context.Background(),30*time.Second); defer cancel()
 dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
 holder,err := pgx.Connect(ctx,dsn); if err != nil { t.Fatal(err) }; defer holder.Close(context.Background())
 observer,err := pgx.Connect(ctx,dsn); if err != nil { t.Fatal(err) }; defer observer.Close(context.Background())
 cfg,err := pgxpool.ParseConfig(dsn); if err != nil { t.Fatal(err) }
 cfg.MaxConns=1; cfg.ConnConfig.RuntimeParams["role"]="photo_api"
 db,err := pgxpool.NewWithConfig(ctx,cfg); if err != nil { t.Fatal(err) }; defer db.Close()
 connection,err := db.Acquire(ctx); if err != nil { t.Fatal(err) }
 readerPID := connection.Conn().PgConn().PID(); connection.Release()
 tx,err := holder.Begin(ctx); if err != nil { t.Fatal(err) }; defer tx.Rollback(context.Background())
 var receipt uuid.UUID
 if err=tx.QueryRow(ctx,"SELECT id FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Generation.CreateCandidate' AND key=$3 FOR UPDATE", f.Tenant,f.Actor,f.Key).Scan(&receipt); err != nil { t.Fatal(err) }
 type outcome struct { body []byte; err error }
 done := make(chan outcome,1)
 submit := (Service{DB:db}).ReplayGeneration
 if target:=os.Getenv("PHOTO_TEST_GENERATION_AUTHORITY_TARGET"); target=="abandon" { submit=(Service{DB:db}).AbandonGeneration } else if target!="" && target!="replay" { t.Fatal("invalid authority target") }
 go func() { response,e := submit(ctx,auth.Identity{UserID:f.Actor,SessionID:f.Session},f.Tenant,f.Key,f.Request); done <- outcome{response.Body,e} }()
 // Cancellation must precede pool.Close even when an assertion fails mid-wait.
 defer cancel()
 ticker := time.NewTicker(10*time.Millisecond); defer ticker.Stop()
 deadline := time.NewTimer(5*time.Second); defer deadline.Stop()
 for {
  var blocked bool
  if err=observer.QueryRow(ctx,"SELECT $1::integer=ANY(pg_blocking_pids($2::integer))",holder.PgConn().PID(),readerPID).Scan(&blocked); err != nil { t.Fatal(err) }
  if blocked { break }
  select { case result:= <-done: t.Fatal("replay did not wait",result.err); case <-deadline.C: t.Fatal("receipt contention not observed"); case <-ticker.C: }
 }
 args := []any{f.Tenant,f.Request.AssetVersionID}
 if mode=="revoked" || mode=="expired" { args=[]any{f.Session} }
 if mode=="tenant_disabled" { args=[]any{f.Tenant} }
 if mode=="role_downgraded" { args=[]any{f.Tenant,f.Actor} }
 tag,err := observer.Exec(ctx,statement,args...); if err != nil || tag.RowsAffected()!=1 { t.Fatal("authority fixture did not change",err) }
 if err=tx.Commit(ctx); err != nil { t.Fatal(err) }
 select {
 case result:= <-done:
  code := "NOT_FOUND"; if mode=="role_downgraded" { code="FORBIDDEN" }
  if result.err==nil || domain.AsError(result.err).Code!=code || len(result.body)!=0 { t.Fatal("saved receipt leaked after authority loss",result.err) }
 case <-ctx.Done(): t.Fatal(ctx.Err())
 }
 t.Log("GENERATION_REPLAY_AUTHORITY_WAIT_OK")
}
