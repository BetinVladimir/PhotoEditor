package maintenance

import (
 "context"
 "errors"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/jackc/pgx/v5/pgconn"
)

// Actual SQL under maintenance role, isolated transactional catalogue fixtures.
// Authored only: no object deletion or physical model/runtime claim is made.
func TestIntegrationResourceWithdrawalRetainsEvidenceAndFencesSettlement(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("disposable fully migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),45*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,kind:=range []string{"project","asset"}{t.Run(kind,func(t *testing.T){
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 var actor,tenant,project,resource,artifact,snapshot,run,job uuid.UUID;var epoch int64
 if err=tx.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@withdrawal.test").Scan(&actor,&epoch);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,actor).Scan(&tenant);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Withdrawal') RETURNING id`,tenant).Scan(&project);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,100,'application/json','working_image','ready',clock_timestamp()) RETURNING id`,tenant,"withdrawal/"+uuid.NewString(),strings.Repeat("a",64)).Scan(&artifact);err!=nil{t.Fatal(err)}
 resource=project
 if kind=="asset"{if err=tx.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,tenant,project,artifact).Scan(&resource);err!=nil{t.Fatal(err)}}
 if err=tx.QueryRow(ctx,`INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,0) RETURNING id`,tenant,actor,strings.Repeat("b",64)).Scan(&snapshot);err!=nil{t.Fatal(err)}
 // A direct project lane deliberately has no collection/snapshot membership.
 config:=`{"project_id":"`+project.String()+`"}`
 if kind=="asset"{var version uuid.UUID;if err=tx.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,tenant,resource,artifact,strings.Repeat("a",64)).Scan(&version);err!=nil{t.Fatal(err)};config=`{"asset_version_id":"`+version.String()+`"}`}
 if err=tx.QueryRow(ctx,`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed,created_at) VALUES($1,'v2','draft',$2,$3::jsonb,$4,$5,'fail_fast',false,'1800-01-01') RETURNING id`,tenant,snapshot,config,strings.Repeat("c",64),actor).Scan(&run);err!=nil{t.Fatal(err)}
 if kind=="project"{_,err=tx.Exec(ctx,`UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=$1`,project)}else{_,err=tx.Exec(ctx,`UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=$1`,resource)};if err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`INSERT INTO photo.deletion_jobs(tenant_id,resource_type,resource_id,requested_by,created_at) VALUES($1,$2,$3,$4,'1900-01-01') RETURNING id`,tenant,kind,resource,actor).Scan(&job);err!=nil{t.Fatal(err)}
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler"}{
  if _,err=tx.Exec(ctx,`SAVEPOINT denied_caller`);err!=nil{t.Fatal(err)}
  if _,err=tx.Exec(ctx,`SET LOCAL ROLE `+role);err!=nil{t.Fatal(err)}
  _,denied:=tx.Exec(ctx,`SELECT photo.claim_resource_withdrawal()`);var pgerr *pgconn.PgError
  if !errors.As(denied,&pgerr)||pgerr.Code!="42501"{t.Fatal("unauthorized cleanup caller",role,denied)}
  if _,err=tx.Exec(ctx,`ROLLBACK TO SAVEPOINT denied_caller`);err!=nil{t.Fatal(err)}
  if _,err=tx.Exec(ctx,`RELEASE SAVEPOINT denied_caller`);err!=nil{t.Fatal(err)}
 }
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_maintenance`);err!=nil{t.Fatal(err)}
 if err=RequireResourceWithdrawal(ctx,tx);err!=nil{t.Fatal(err)}
 var claimed,token,claimedTenant,claimedResource uuid.UUID;var claimedKind string
 if err=tx.QueryRow(ctx,`SELECT job_id,cleanup_token,tenant_id,resource_type,resource_id FROM photo.claim_resource_withdrawal()`).Scan(&claimed,&token,&claimedTenant,&claimedKind,&claimedResource);err!=nil||claimed!=job||claimedTenant!=tenant||claimedKind!=kind||claimedResource!=resource{t.Fatal("wrong claim",err)}
 finish:=func(tok,tid uuid.UUID)bool{t.Helper();var ok bool;if e:=tx.QueryRow(ctx,`SELECT photo.finish_resource_withdrawal($1,$2,$3,$4,$5)`,tid,job,kind,resource,tok).Scan(&ok);e!=nil{t.Fatal(e)};return ok}
 if finish(uuid.New(),tenant)||finish(token,uuid.New()){t.Fatal("foreign/stale authority accepted")}
 oldToken:=token
 if _,err=tx.Exec(ctx,`RESET ROLE`);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`UPDATE photo.deletion_jobs SET cleanup_lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`,job);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_maintenance`);err!=nil{t.Fatal(err)}
 if finish(oldToken,tenant){t.Fatal("expired lease completed")}
 if err=tx.QueryRow(ctx,`SELECT cleanup_token FROM photo.claim_resource_withdrawal()`).Scan(&token);err!=nil{t.Fatal(err)}
 if token==oldToken||finish(oldToken,tenant){t.Fatal("reclaimed token did not fence old owner")}
 if finish(token,tenant){t.Fatal("live run completed withdrawal early")}
 if _,err=tx.Exec(ctx,`RESET ROLE`);err!=nil{t.Fatal(err)}
 var cancelling bool;if err=tx.QueryRow(ctx,`SELECT cancel_requested AND status='cancelling' FROM photo.runs WHERE id=$1`,run).Scan(&cancelling);err!=nil||!cancelling{t.Fatal("ordinary cancellation missing",err)}
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_maintenance`);err!=nil{t.Fatal(err)}
 var settled int;if err=tx.QueryRow(ctx,`SELECT photo.finalize_run_cancellation()`).Scan(&settled);err!=nil||settled!=1{t.Fatal("ordinary draft cancellation did not settle",settled,err)}
 if _,err=tx.Exec(ctx,`RESET ROLE`);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT status='cancelled' AND graph_sealed AND (SELECT count(*) FROM photo.run_events e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id AND e.type='run.cancelled')=1 FROM photo.runs r WHERE id=$1`,run).Scan(&cancelling);err!=nil||!cancelling{t.Fatal("unsealed draft lacks ordinary terminal/event",err)}
 if _,err=tx.Exec(ctx,`UPDATE photo.deletion_jobs SET resource_cleanup_retry_at=NULL WHERE id=$1`,job);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_maintenance`);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT cleanup_token FROM photo.claim_resource_withdrawal()`).Scan(&token);err!=nil{t.Fatal(err)}
 if !finish(token,tenant)||finish(token,tenant){t.Fatal("completion not exactly once")}
 if _,err=tx.Exec(ctx,`RESET ROLE`);err!=nil{t.Fatal(err)}
 var retained bool;var cursor string
 if err=tx.QueryRow(ctx,`SELECT state='ready' AND object_version='v1' AND bytes=100 FROM photo.artifacts WHERE tenant_id=$1 AND id=$2`,tenant,artifact).Scan(&retained);err!=nil||!retained{t.Fatal("retained original changed",err)}
 if err=tx.QueryRow(ctx,`SELECT cursor FROM photo.deletion_jobs WHERE id=$1`,job).Scan(&cursor);err!=nil||!strings.Contains(cursor,"logical_withdrawal_settled"){t.Fatal("physical deletion falsely claimed",cursor,err)}
 })}
}
