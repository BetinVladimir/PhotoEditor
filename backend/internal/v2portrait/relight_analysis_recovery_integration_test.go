package v2portrait

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestIntegrationRelightAnalysisStoredRecovery(t *testing.T){
 path:=os.Getenv("PHOTO_TEST_RELIGHT_ANALYSIS_RECOVERY_SEED");if path==""{t.Skip("isolated SQL/object fixture required")}
 raw,err:=os.ReadFile(path);if err!=nil{t.Fatal("fixture unavailable")}
 var seed struct {
  Tenant uuid.UUID `json:"tenant"`;Project uuid.UUID `json:"project"`;Actor uuid.UUID `json:"actor"`;Session uuid.UUID `json:"session"`;Snapshot uuid.UUID `json:"snapshot"`
  Operation string `json:"operation"`;Base v2input.SessionVersionBinding `json:"base"`;Runtime RelightAnalysisRuntime `json:"runtime"`;Capacity LocalResourceEstimate `json:"capacity"`;Dataset string `json:"dataset_sha"`;Issuer uuid.UUID `json:"issuer"`
 }
 if json.Unmarshal(raw,&seed)!=nil {t.Fatal("invalid fixture")}
 ctx,cancel:=context.WithTimeout(context.Background(),80*time.Second);defer cancel()
 config,err:=pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"));if err!=nil{t.Fatal("database config")};config.ConnConfig.RuntimeParams["role"]="photo_scheduler"
 pool,err:=pgxpool.NewWithConfig(ctx,config);if err!=nil{t.Fatal("scheduler pool")};defer pool.Close()
 admin,err:=pgx.Connect(ctx,os.Getenv("PHOTO_TEST_DATABASE_URL"));if err!=nil{t.Fatal("control connection")};defer admin.Close(context.Background())
 store,err:=objectstore.New(objectstore.Config{Endpoint:os.Getenv("PHOTO_TEST_MINIO_URL"),Bucket:os.Getenv("PHOTO_TEST_MINIO_BUCKET"),AccessKey:os.Getenv("PHOTO_TEST_MINIO_ACCESS"),SecretKey:os.Getenv("PHOTO_TEST_MINIO_SECRET"),Development:true});if err!=nil{t.Fatal("object store")}
 expected:=v2qualification.Expected{Operation:seed.Operation,Version:"1",Backend:seed.Runtime.Backend,ModelLockSHA:seed.Runtime.ModelLockSHA,RuntimeLockSHA:seed.Runtime.RuntimeLockSHA,DeviceProfileSHA:seed.Runtime.DeviceProfileSHA,DatasetSHA:seed.Dataset,IssuerID:seed.Issuer,RequiredChecks:[]string{"quality"},TestFixtureSHAs:map[string]struct{}{strings.Repeat("e",64):{}},EvidenceSHAs:map[string]struct{}{strings.Repeat("f",64):{}},CalibrationSHAs:map[string]struct{}{strings.Repeat("6",64):{}},LicenseCleared:true}
 service,err:=NewRelightAnalysisService(RelightAnalysisCoordinator{DB:pool,Reader:store,Writer:store,Bucket:os.Getenv("PHOTO_TEST_MINIO_BUCKET"),Expected:expected},seed.Tenant,seed.Project,seed.Runtime,seed.Capacity);if err!=nil{t.Fatal("service construction",err)}
 who:=auth.Identity{UserID:seed.Actor,SessionID:seed.Session};intent:=RelightAnalysisIntent{SchemaVersion:1,Snapshot:seed.Snapshot,Base:seed.Base,Operation:seed.Operation};key:="analysis-qualified-recovery"
 first,err:=service.SubmitIntent(ctx,who,key,intent);if err!=nil{t.Fatal("real analysis admission",err)}
 expectedReceipt,err:=canonical.Marshal(first);if err!=nil{t.Fatal(err)}
 snapshot:=func()[]int64{
  var tasks,events,outbox,artifacts int64
  err:=admin.QueryRow(ctx,`SELECT (SELECT count(*) FROM photo.tasks WHERE run_id=$1),(SELECT count(*) FROM photo.run_events WHERE run_id=$1 AND type='v2.run.queued'),(SELECT count(*) FROM photo.outbox WHERE tenant_id=$2 AND routing_key='v2.run.queued' AND payload->>'run_id'=$3),(SELECT count(*) FROM photo.artifacts WHERE tenant_id=$2)`,first.Run,seed.Tenant,first.Run.String()).Scan(&tasks,&events,&outbox,&artifacts)
  if err!=nil||tasks!=1||events!=1||outbox!=1{t.Fatal("admission inventory",err,tasks,events,outbox)}
  return []int64{tasks,events,outbox,artifacts}
 }
 before,_:=canonical.Marshal(snapshot())
 for _,mode:=range []string{"removed","replaced"}{
  router,_:=NewRelightAnalysisReceiptOnlyProjects(pool)
  if mode=="replaced"{replacement:=*service;replacement.runtime.ModelLockSHA=strings.Repeat("9",64);router.services[analysisProjectKey{seed.Tenant,seed.Project,seed.Operation}]=&replacement}
  receipt,readErr:=router.SubmitProjectAnalysis(WithRelightReplayOnly(ctx),who,seed.Tenant,seed.Project,key,intent)
  actual,_:=canonical.Marshal(receipt);if readErr!=nil||string(actual)!=string(expectedReceipt){t.Fatal("stored qualified recovery",mode,readErr)}
  changed:=intent;changed.Base.CanvasHash=strings.Repeat("0",64)
  if _,readErr=router.SubmitProjectAnalysis(WithRelightReplayOnly(ctx),who,seed.Tenant,seed.Project,key,changed);!errors.Is(readErr,v2input.ErrConflict){t.Fatal("changed original intent recovered",mode,readErr)}
  if _,readErr=router.SubmitProjectAnalysis(WithRelightReplayOnly(ctx),who,seed.Tenant,seed.Project,"never-admitted-key",intent);!errors.Is(readErr,v2input.ErrExpired){t.Fatal("absence admitted new work",mode,readErr)}
  after,_:=canonical.Marshal(snapshot());if string(before)!=string(after){t.Fatal("replay changed task/event/outbox/artifact inventory",mode)}
 }
 if _,err=admin.Exec(ctx,"UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1",seed.Session);err!=nil{t.Fatal(err)}
 router,_:=NewRelightAnalysisReceiptOnlyProjects(pool)
 if _,err=router.SubmitProjectAnalysis(WithRelightReplayOnly(ctx),who,seed.Tenant,seed.Project,key,intent);err==nil{t.Fatal("revoked session recovered receipt")}
}
