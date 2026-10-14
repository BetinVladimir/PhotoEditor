package interchange
import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationArchiveUploadLedgerRefusesMissingAuthority(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01593 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,signature:=range []string{"photo.reserve_v30_archive_upload(uuid,uuid,uuid,text,text,text)","photo.bind_v30_archive_multipart(uuid,uuid,uuid,uuid,uuid,text,text)"}{
  for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)};if allowed!=(role=="photo_api"){t.Fatal("unexpected privilege",role,signature)}}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx);tid:=uuid.New()
 if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 var outcome string;var upload,token *uuid.UUID;var state,multipart *string
 if err:=tx.QueryRow(ctx,`SELECT outcome,upload_id,token,state,multipart_id FROM photo.reserve_v30_archive_upload($1,$2,$3,'key','{}','photos')`,tid,uuid.New(),uuid.New()).Scan(&outcome,&upload,&token,&state,&multipart);err!=nil{t.Fatal(err)}
 if outcome!="invalid"||upload!=nil||token!=nil{t.Fatal("reserved invalid source",outcome)}
 var count int;if err:=tx.QueryRow(ctx,`SELECT count(*) FROM photo.v30_archive_uploads WHERE tenant_id=$1`,tid).Scan(&count);err!=nil{t.Fatal(err)};if count!=0{t.Fatal("denied call left ledger")}
}

func TestIntegrationArchiveCompletionLeaseRoleIsolation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01594 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for signature,owner:=range map[string]string{
  "photo.queue_v30_archive_completion(uuid,uuid,uuid,uuid,text,bigint,jsonb)":"photo_api",
  "photo.claim_v30_archive_completion()":"photo_scheduler",
  "photo.renew_v30_archive_completion(uuid,uuid,uuid)":"photo_scheduler",
  "photo.release_v30_archive_completion(uuid,uuid,uuid)":"photo_scheduler",
 }{for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)};if allowed!=(role==owner){t.Fatal("unexpected completion privilege",role,signature)}}}
 tid,upload,token:=uuid.New(),uuid.New(),uuid.New()
 for _,name:=range []string{"renew_v30_archive_completion","release_v30_archive_completion"}{var accepted bool
  if err:=db.QueryRow(ctx,"SELECT photo."+name+"($1,$2,$3)",tid,upload,token).Scan(&accepted);err!=nil{t.Fatal(err)};if accepted{t.Fatal("accepted absent lease",name)}
 }
}

func TestIntegrationArchiveAdoptionRejectsAbsentLeaseAndCrossProject(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01595 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tid,upload,token:=uuid.New(),uuid.New(),uuid.New();var accepted bool
 if err:=db.QueryRow(ctx,`SELECT photo.finish_v30_archive_completion($1,$2,$3,'photos','foreign','v1',22,$4)`,tid,upload,token,rawSHA([]byte("source"))).Scan(&accepted);err!=nil{t.Fatal(err)}
 if accepted{t.Fatal("adopted absent lease")}
 for signature,owner:=range map[string]string{"photo.finish_v30_archive_completion(uuid,uuid,uuid,text,text,text,bigint,text)":"photo_scheduler","photo.v30_uploaded_archive_ready(uuid,uuid,uuid,uuid,uuid,text)":"photo_api"}{
  for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)};if allowed!=(role==owner){t.Fatal("unexpected archive adoption privilege",role)}}
 }
 if err:=db.QueryRow(ctx,`SELECT photo.v30_uploaded_archive_ready($1,$2,$3,$4,$5,$6)`,tid,uuid.New(),uuid.New(),uuid.New(),uuid.New(),rawSHA([]byte("source"))).Scan(&accepted);err!=nil{t.Fatal(err)};if accepted{t.Fatal("foreign project archive visible")}
}

func TestIntegrationImportArchiveGateRejectsAbsentAndForeignSources(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01596 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.v30_import_archive_ready(uuid,uuid,uuid,uuid,uuid,text)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)};if allowed!=(role=="photo_api"){t.Fatal("unexpected archive gate privilege",role)}}
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx);tid:=uuid.New()
 if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 for _,queryTenant:=range []uuid.UUID{tid,uuid.New()}{var accepted bool;if err:=tx.QueryRow(ctx,`SELECT photo.v30_import_archive_ready($1,$2,$3,$4,$5,$6)`,queryTenant,uuid.New(),uuid.New(),uuid.New(),uuid.New(),rawSHA([]byte("archive"))).Scan(&accepted);err!=nil{t.Fatal(err)};if accepted{t.Fatal("accepted absent/foreign archive")}}
}

func TestIntegrationArchiveIntegrityFailureRequiresSchedulerLease(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01597 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.fail_v30_archive_completion(uuid,uuid,uuid)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)};if allowed!=(role=="photo_scheduler"){t.Fatal("unexpected failure privilege",role)}
 }
 var accepted bool;if err:=db.QueryRow(ctx,`SELECT photo.fail_v30_archive_completion($1,$2,$3)`,uuid.New(),uuid.New(),uuid.New()).Scan(&accepted);err!=nil{t.Fatal(err)};if accepted{t.Fatal("failed absent lease")}
}

func TestIntegrationArchiveCleanupRoleAndAbsentLease(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01598 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,signature:=range []string{"photo.claim_v30_archive_cleanup()","photo.renew_v30_archive_cleanup(uuid,uuid,uuid)","photo.release_v30_archive_cleanup(uuid,uuid,uuid)","photo.finish_v30_archive_cleanup(uuid,uuid,uuid,text,text)"}{
  for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
   if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role=="photo_scheduler"){t.Fatal("unexpected cleanup privilege",role,signature)}
  }
 }
 for _,name:=range []string{"renew_v30_archive_cleanup","release_v30_archive_cleanup"}{var accepted bool
  if err:=db.QueryRow(ctx,"SELECT photo."+name+"($1,$2,$3)",uuid.New(),uuid.New(),uuid.New()).Scan(&accepted);err!=nil{t.Fatal(err)};if accepted{t.Fatal("accepted absent cleanup lease")}
 }
 var accepted bool
 if err:=db.QueryRow(ctx,`SELECT photo.finish_v30_archive_cleanup($1,$2,$3,'photos','foreign')`,uuid.New(),uuid.New(),uuid.New()).Scan(&accepted);err!=nil{t.Fatal(err)}
 if accepted{t.Fatal("released quota without cleanup lease")}
}

func TestIntegrationArchivePartAuthorizationRoleAndMissingUpload(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01599 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.authorize_v30_archive_parts(uuid,uuid,uuid,uuid,uuid,text,integer[])"
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("unexpected signing privilege",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx);tid:=uuid.New()
 if _,err:=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 for _,parts:=range [][]int32{nil,{}, {0},{1,1},{1}}{var count int
  if err:=tx.QueryRow(ctx,`SELECT count(*) FROM photo.authorize_v30_archive_parts($1,$2,$3,$4,$5,$6,$7)`,tid,uuid.New(),uuid.New(),uuid.New(),uuid.New(),rawSHA([]byte("request")),parts).Scan(&count);err!=nil{t.Fatal(err)}
  if count!=0{t.Fatal("signed missing or invalid upload",parts)}
 }
}

func TestIntegrationArchiveLookupRoleAndAbsentScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01600 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.lookup_v30_archive_upload(uuid,uuid,uuid,uuid,uuid)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("unexpected lookup privilege",role)}
 }
 var count int
 if err:=db.QueryRow(ctx,`SELECT count(*) FROM photo.lookup_v30_archive_upload($1,$2,$3,$4,$5)`,uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New()).Scan(&count);err!=nil{t.Fatal(err)}
 if count!=0{t.Fatal("lookup exposed absent/foreign upload")}
}

func TestIntegrationArchiveExpirySchedulerOnly(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01601 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.expire_v30_archive_upload()','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler"){t.Fatal("unexpected expiry privilege",role)}
 }
}

func TestIntegrationArchiveAbortAPIOnlyAndAbsentUpload(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01602 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.abort_v30_archive_upload(uuid,uuid,uuid,uuid,text)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("unexpected abort privilege",role)}
 }
 var accepted bool
 if err:=db.QueryRow(ctx,`SELECT photo.abort_v30_archive_upload($1,$2,$3,$4,$5)`,uuid.New(),uuid.New(),uuid.New(),uuid.New(),rawSHA([]byte("request"))).Scan(&accepted);err!=nil{t.Fatal(err)}
 if accepted{t.Fatal("aborted absent upload")}
}

func TestIntegrationArchiveCleanupAuthoritySchedulerOnly(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01603 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler"){t.Fatal("unexpected drain privilege",role)}
 }
 var allowed bool
 if err:=db.QueryRow(ctx,`SELECT photo.v30_archive_cleanup_authorized($1,$2,$3,'photos','foreign')`,uuid.New(),uuid.New(),uuid.New()).Scan(&allowed);err!=nil{t.Fatal(err)};if allowed{t.Fatal("authorized absent cleanup")}
}

func TestIntegrationArchiveWriterLedgerRolesAndMissingScope(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01604 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for signature,owner:=range map[string]string{
  "photo.start_v30_archive_begin(uuid,uuid,uuid,uuid,uuid)":"photo_api",
  "photo.start_v30_archive_complete(uuid,uuid,uuid)":"photo_scheduler",
  "photo.finish_v30_archive_begin_writer(uuid,uuid,uuid,text)":"photo_api",
  "photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,text)":"",
 }{for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)};if allowed!=(role==owner){t.Fatal("unexpected writer privilege",role,signature)}
 }}
 var ticket *uuid.UUID
 if err:=db.QueryRow(ctx,`SELECT photo.start_v30_archive_complete($1,$2,$3)`,uuid.New(),uuid.New(),uuid.New()).Scan(&ticket);err!=nil{t.Fatal(err)};if ticket!=nil{t.Fatal("writer for absent upload")}
}

func TestIntegrationArchiveBeginRecoveryAPIOnly(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01605 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.recover_v30_archive_begin(uuid,uuid,uuid,uuid,uuid)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("unexpected recovery privilege",role)}
 }
}

func TestIntegrationArchiveCompleteRecoverySchedulerOnly(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01606 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.recover_v30_archive_complete(uuid,uuid,uuid)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler"){t.Fatal("unexpected recovery privilege",role)}
 }
 var version *string
 if err:=db.QueryRow(ctx,`SELECT photo.recover_v30_archive_complete($1,$2,$3)`,uuid.New(),uuid.New(),uuid.New()).Scan(&version);err!=nil{t.Fatal(err)};if version!=nil{t.Fatal("recovered absent upload")}
}

func TestIntegrationArchivePublicationWriterFenceInstalled(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01607 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 // Installation evidence only; successful publication and races require fixtures.
 var installed bool
 if err:=db.QueryRow(ctx,`SELECT position($needle$w.state='open'$needle$ in pg_get_functiondef('photo.finish_v30_archive_completion(uuid,uuid,uuid,text,text,text,bigint,text)'::regprocedure))>0
 AND position('w.receipt=p_version' in pg_get_functiondef('photo.finish_v30_archive_completion(uuid,uuid,uuid,text,text,text,bigint,text)'::regprocedure))>0`).Scan(&installed);err!=nil{t.Fatal(err)}
 if !installed{t.Fatal("publication writer receipt fence absent")}
 var accepted bool
 if err:=db.QueryRow(ctx,`SELECT photo.finish_v30_archive_completion($1,$2,$3,'photos','foreign','version',22,$4)`,uuid.New(),uuid.New(),uuid.New(),"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&accepted);err!=nil{t.Fatal(err)}
 if accepted{t.Fatal("published absent scoped upload/writer")}
}

func TestIntegrationArchiveCleanupCommitAuthorityInstalled(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01608 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 // Installation assertion only; not physical cleanup or quota lifecycle proof.
 var installed bool
 if err:=db.QueryRow(ctx,`SELECT position('photo.v30_archive_cleanup_authorized(p_tenant,p_upload,p_token,p_bucket,p_key)' in pg_get_functiondef('photo.finish_v30_archive_cleanup(uuid,uuid,uuid,text,text)'::regprocedure))>0`).Scan(&installed);err!=nil{t.Fatal(err)}
 if !installed{t.Fatal("cleanup commit authority absent")}
 var accepted bool
 if err:=db.QueryRow(ctx,`SELECT photo.finish_v30_archive_cleanup($1,$2,$3,'photos','foreign')`,uuid.New(),uuid.New(),uuid.New()).Scan(&accepted);err!=nil{t.Fatal(err)}
 if accepted{t.Fatal("cleanup commit accepted absent upload")}
}

func TestIntegrationArchiveCompleteAdmissionCurrentAuthorityInstalled(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01609 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 // Only verifies migration installation; session-revocation races need fixtures.
 for _,signature:=range []string{"photo.start_v30_archive_complete(uuid,uuid,uuid)","photo.recover_v30_archive_complete(uuid,uuid,uuid)"}{
  var installed bool
  if err:=db.QueryRow(ctx,`SELECT position('photo.v2_editor_authorized(p_tenant,d.project_id,d.actor_id,d.completion_session_id)' in pg_get_functiondef($1::regprocedure))>0
 AND position('download_allowed FOR SHARE' in pg_get_functiondef($1::regprocedure))>0`,signature).Scan(&installed);err!=nil{t.Fatal(err)}
  if !installed{t.Fatal("current authority admission absent",signature)}
 }
}

func TestIntegrationArchiveCompletionRetryDefaultsAndPrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01610 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var required bool
 if err:=db.QueryRow(ctx,`SELECT is_nullable='NO' AND column_default IS NOT NULL FROM information_schema.columns WHERE table_schema='photo' AND table_name='v30_archive_uploads' AND column_name='completion_retry_at'`).Scan(&required);err!=nil{t.Fatal(err)}
 if !required{t.Fatal("durable retry horizon not installed")}
 for _,signature:=range []string{"photo.claim_v30_archive_completion()","photo.release_v30_archive_completion(uuid,uuid,uuid)"}{
  for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
   if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role=="photo_scheduler"){t.Fatal("retry lifecycle privilege widened",role,signature)}
  }
 }
}

func TestIntegrationArchiveDrainScopeSchedulerOnly(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01611 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.v30_archive_drain_multipart(uuid,uuid,text,text)','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler"){t.Fatal("drain scope privilege widened",role)}
 }
 var multipart *string
 if err:=db.QueryRow(ctx,`SELECT photo.v30_archive_drain_multipart($1,$2,'photos','foreign')`,uuid.New(),uuid.New()).Scan(&multipart);err!=nil{t.Fatal(err)}
 if multipart!=nil{t.Fatal("drain authorized absent upload")}
}

func TestIntegrationArchiveCrashRecoverySchedulerOnly(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01613 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,signature:=range []string{"photo.claim_v30_archive_completion_recovery()","photo.adopt_v30_archive_complete_writer(uuid,uuid,uuid)"}{
  for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
   if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role=="photo_scheduler"){t.Fatal("recovery privilege widened",role,signature)}
  }
 }
 var ticket *uuid.UUID
 if err:=db.QueryRow(ctx,`SELECT photo.adopt_v30_archive_complete_writer($1,$2,$3)`,uuid.New(),uuid.New(),uuid.New()).Scan(&ticket);err!=nil{t.Fatal(err)}
 if ticket!=nil{t.Fatal("adopted writer without upload/lease")}
}

func TestIntegrationArchiveCompleteRevocationSchedulerOnly(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01614 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,signature:=range []string{"photo.claim_v30_archive_complete_revocation()","photo.finish_v30_archive_complete_revocation(uuid,uuid,uuid,uuid,text,text,text)"}{
  for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
   if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role=="photo_scheduler"){t.Fatal("revocation privilege widened",role,signature)}
  }
 }
 var accepted bool
 if err:=db.QueryRow(ctx,`SELECT photo.finish_v30_archive_complete_revocation($1,$2,$3,$4,'photos','foreign','multipart')`,uuid.New(),uuid.New(),uuid.New(),uuid.New()).Scan(&accepted);err!=nil{t.Fatal(err)}
 if accepted{t.Fatal("revoked absent writer")}
}

func TestIntegrationArchiveAuthorityInvalidationSchedulerOnly(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01615 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.invalidate_v30_archive_authority()','EXECUTE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_scheduler"){t.Fatal("invalidation privilege widened",role)}
 }
}

func TestIntegrationArchiveUploadSessionAuthorityInstalled(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01616 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var nullable bool
 if err:=db.QueryRow(ctx,`SELECT is_nullable='YES' FROM information_schema.columns WHERE table_schema='photo' AND table_name='v30_archive_uploads' AND column_name='authority_session_id'`).Scan(&nullable);err!=nil{t.Fatal(err)}
 if !nullable{t.Fatal("legacy unknown session compatibility absent")}
 // Session pins are private, not direct API table privileges.
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_column_privilege($1,'photo.v30_archive_uploads','authority_session_id','SELECT')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed{t.Fatal("private authority session exposed",role)}
 }
}

func TestIntegrationArchiveWriterCloseLeaseFenceSchedulerOnly(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01617 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var old,new bool
  if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,text)','EXECUTE'),has_function_privilege($1,'photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,uuid,text)','EXECUTE')`,role).Scan(&old,&new);err!=nil{t.Fatal(err)}
  if old||new!=(role=="photo_scheduler"){t.Fatal("unfenced close still callable",role)}
 }
 var closed bool
 if err:=db.QueryRow(ctx,`SELECT photo.finish_v30_archive_complete_writer($1,$2,$3,$4,'version')`,uuid.New(),uuid.New(),uuid.New(),uuid.New()).Scan(&closed);err!=nil{t.Fatal(err)}
 if closed{t.Fatal("closed writer without scoped completion lease")}
}

func TestIntegrationArchiveWriterTenantPurgeAllowlistOrder(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01604 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var definition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)`).Scan(&definition);err!=nil{t.Fatal(err)}
 start:=strings.Index(definition,"tables_to_purge constant text[]:=ARRAY[")
 if start<0{t.Fatal("explicit purge allowlist absent")}
 end:=strings.Index(definition[start:],"];")
 if end<0{t.Fatal("purge allowlist not closed")}
 allowlist:=definition[start:start+end]
 writer:=strings.Index(allowlist,"'v30_archive_writers'")
 upload:=strings.Index(allowlist,"'v30_archive_uploads'")
 if writer<0||upload<0||writer>=upload{t.Fatal("archive writer must be deleted before upload ledger")}
 for _,role:=range []string{"photo_fn_owner","photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT has_table_privilege($1,'photo.v30_archive_writers','DELETE')`,role).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_fn_owner"){t.Fatal("unexpected archive writer deletion privilege",role)}
 }
}

func TestIntegrationArchiveUploadErrorStateConstraint(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01774 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var definition string;var validated bool
 err=db.QueryRow(ctx,`SELECT pg_get_constraintdef(c.oid),c.convalidated
 FROM pg_constraint c JOIN pg_class t ON t.oid=c.conrelid JOIN pg_namespace n ON n.oid=t.relnamespace
 WHERE n.nspname='photo' AND t.relname='v30_archive_uploads' AND c.conname='v30_archive_upload_error_state_check'`).Scan(&definition,&validated)
 if err!=nil{t.Fatal(err)}
 if !validated||!strings.Contains(definition,"aborting")||!strings.Contains(definition,"aborted")||!strings.Contains(definition,"error_code IS NOT NULL"){
  t.Fatal("archive error/state ledger constraint missing or unvalidated",definition,validated)
 }
}

func TestIntegrationTenantFinishRetainsOpenArchiveWriterFence(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01775 required")};ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var definition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)`).Scan(&definition);err!=nil{t.Fatal(err)}
 guard:="IF EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=j.tenant_id AND w.state='open') THEN RETURN false;END IF;"
 guardIndex:=strings.Index(definition,guard)
 purgeIndex:=strings.Index(definition,"SET CONSTRAINTS ALL DEFERRED;")
 if guardIndex<0||purgeIndex<0||guardIndex>=purgeIndex{t.Fatal("open archive writer is not fenced before tenant metadata purge")}
}
