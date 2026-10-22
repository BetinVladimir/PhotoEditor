package style

import (
 "context"
 "os"
 "testing"
 "time"
 "fmt"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
)

// Authored SQL race: the reader must wait on the actual set row, then read
// the committed revision and reauthorize project access after that wait.
func TestIntegrationTrainingSetSharedSnapshotAfterWait(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
 cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
 api,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer api.Close()
 for _,archive:=range []bool{false,true}{t.Run(map[bool]string{false:"committed_revision",true:"project_closed_during_wait"}[archive],func(t *testing.T){
  var user,tid,project,style,set uuid.UUID;var epoch int64
  if err:=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@training-snapshot.test").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,user).Scan(&tid);err!=nil{t.Fatal(err)}
  session:=uuid.New();if _,err:=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,user,auth.HashSecret(uuid.NewString()),uuid.New());err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Training','workspace') RETURNING id`,tid).Scan(&project);err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES($1,$2,'Training',$3) RETURNING id`,tid,project,user).Scan(&style);err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(ctx,`INSERT INTO photo.training_sets(tenant_id,style_id) VALUES($1,$2) RETURNING id`,tid,style).Scan(&set);err!=nil{t.Fatal(err)}
  writer,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer writer.Rollback(ctx)
  var pid int;if err:=writer.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil{t.Fatal(err)}
  if _,err:=writer.Exec(ctx,`UPDATE photo.training_sets SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,set);err!=nil{t.Fatal(err)}
  type result struct{value TrainingSet;err error};done:=make(chan result,1)
  go func(){v,e:=(Service{DB:api}).GetTrainingSet(ctx,auth.Identity{UserID:user,SessionID:session,Epoch:epoch},tid,set,100,"");done<-result{v,e}}()
  deadline:=time.Now().Add(5*time.Second)
  for {var waiting bool;if err:=admin.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,pid).Scan(&waiting);err!=nil{t.Fatal(err)};if waiting{break};if time.Now().After(deadline){t.Fatal("reader did not wait on set snapshot")};select{case early:=<-done:t.Fatalf("reader escaped row lock: %v",early.err);case <-time.After(10*time.Millisecond):}}
  if archive{if _,err:=writer.Exec(ctx,`UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`,tid,project);err!=nil{t.Fatal(err)}}
  if err:=writer.Commit(ctx);err!=nil{t.Fatal(err)}
  select{case got:=<-done:if archive{if got.err==nil{t.Fatal("closed project published training snapshot")}}else if got.err!=nil||got.value.Revision!="2"||got.value.Pairs.Revision!="2"{t.Fatalf("incoherent training snapshot: %+v %v",got.value,got.err)};case <-ctx.Done():t.Fatal(ctx.Err())}
  if archive{return}
  var versions [2]uuid.UUID
  for side:=range versions{var artifact,asset uuid.UUID;digest:=strings.Repeat(fmt.Sprint(side+1),64)
   if err:=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES($1,'original','test',$2,$3,1,'image/jpeg','ready','v1') RETURNING id`,tid,"training/"+uuid.NewString(),digest).Scan(&artifact);err!=nil{t.Fatal(err)}
   if err:=admin.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,tid,project,artifact).Scan(&asset);err!=nil{t.Fatal(err)}
   if err:=admin.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,tid,asset,artifact,digest).Scan(&versions[side]);err!=nil{t.Fatal(err)}
  }
  var pair uuid.UUID;if err:=admin.QueryRow(ctx,`INSERT INTO photo.training_pairs(tenant_id,training_set_id,before_asset_version_id,after_asset_version_id,session_id,confirmed,included) VALUES($1,$2,$3,$4,'scene',true,true) RETURNING id`,tid,set,versions[0],versions[1]).Scan(&pair);err!=nil{t.Fatal(err)}
  for _,revoke:=range []bool{false,true}{
   blocker,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer blocker.Rollback(ctx)
   var blockerPID int;if err:=blocker.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blockerPID);err!=nil{t.Fatal(err)}
   var locked uuid.UUID;if err:=blocker.QueryRow(ctx,`SELECT id FROM photo.training_pairs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,pair).Scan(&locked);err!=nil{t.Fatal(err)}
   expected:=int64(1);if revoke{expected=2};no:=false;updates:=make(chan error,1)
   go func(){_,e:=(Service{DB:api}).UpdatePair(ctx,auth.Identity{UserID:user,SessionID:session,Epoch:epoch},tid,set,pair,expected,PairPatch{ExpectedRevision:expected,Confirmed:&no});updates<-e}()
   until:=time.Now().Add(5*time.Second);for{var waits bool;if err:=admin.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,blockerPID).Scan(&waits);err!=nil{t.Fatal(err)};if waits{break};if time.Now().After(until){t.Fatal("pair mutation did not wait")};select{case e:=<-updates:t.Fatalf("pair writer escaped lock: %v",e);case <-time.After(10*time.Millisecond):}}
   if revoke{if _,err:=blocker.Exec(ctx,`UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`,tid,project);err!=nil{t.Fatal(err)}}else{if _,err:=blocker.Exec(ctx,`UPDATE photo.training_pairs SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,pair);err!=nil{t.Fatal(err)}}
   if err:=blocker.Commit(ctx);err!=nil{t.Fatal(err)}
   select{case e:=<-updates:if e==nil{t.Fatal("pair update ignored stale CAS or revoked project")};case <-ctx.Done():t.Fatal(ctx.Err())}
   var confirmed bool;var revision int64;if err:=admin.QueryRow(ctx,`SELECT confirmed,revision FROM photo.training_pairs WHERE tenant_id=$1 AND id=$2`,tid,pair).Scan(&confirmed,&revision);err!=nil{t.Fatal(err)};if !confirmed||revision!=2{t.Fatal("refused pair mutation changed stored pair",confirmed,revision)}
  }
 })}
}
