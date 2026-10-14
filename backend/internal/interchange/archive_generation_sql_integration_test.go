package interchange

import (
 "context"
 "os"
 "strings"
 "testing"

 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationArchiveGenerationCompletionKeyFences(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migrations through 01932 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for signature,owner:=range map[string]string{
  "photo.start_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid)":"",
  "photo.start_v30_archive_begin_generation_checked(uuid,uuid,uuid,uuid,uuid,text)":"photo_api",
  "photo.finish_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text)":"photo_api",
  "photo.bind_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text)":"photo_api",
  "photo.read_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid)":"",
  "photo.read_v30_archive_begin_generation_checked(uuid,uuid,uuid,uuid,uuid,text)":"photo_api",
  "photo.v30_archive_begin_generation_status(uuid,uuid,uuid,uuid,uuid,uuid,text)":"photo_api",
  "photo.finish_v30_archive_begin_not_dispatched(uuid,uuid,uuid,uuid,uuid,uuid,uuid)":"photo_api",
  "photo.v30_archive_open_begin_for_status(uuid,uuid,uuid,uuid,uuid,uuid)":"photo_api",
  "photo.v30_archive_cross_key_reconciliation_required(uuid,uuid,uuid,uuid,uuid)":"photo_api",
  "photo.v30_archive_storage_key_current(uuid,uuid,text,text)":"",
 }{
  var definition,actualOwner,settings string;var secdef bool
  err:=db.QueryRow(ctx,`SELECT pg_get_functiondef(p.oid),pg_get_userbyid(p.proowner),p.prosecdef,coalesce(array_to_string(p.proconfig,','),'') FROM pg_proc p WHERE p.oid=$1::regprocedure`,signature).Scan(&definition,&actualOwner,&secdef,&settings)
  if err!=nil{t.Fatal(signature,err)}
  if actualOwner!="photo_fn_owner"||!secdef||!strings.Contains(settings,"search_path=pg_catalog")||!strings.Contains(settings,"photo"){t.Fatal("generation function installation",signature)}
  _=definition
  for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
   var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role==owner){t.Fatal("generation function privilege",signature,role,allowed)}
  }
 }
 for _,signature:=range []string{
  "photo.queue_v30_archive_completion(uuid,uuid,uuid,uuid,text,bigint,jsonb)",
  "photo.claim_v30_archive_completion()",
  "photo.claim_v30_archive_completion_recovery()",
  "photo.start_v30_archive_complete(uuid,uuid,uuid)",
  "photo.recover_v30_archive_complete(uuid,uuid,uuid)",
  "photo.adopt_v30_archive_complete_writer(uuid,uuid,uuid)",
  "photo.finish_v30_archive_completion(uuid,uuid,uuid,text,text,text,bigint,text)",
 }{
  var definition string;if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef($1::regprocedure)`,signature).Scan(&definition);err!=nil{t.Fatal(signature,err)}
  if !strings.Contains(definition,"photo.v30_archive_storage_key_current("){t.Fatal("completion key fence absent",signature)}
 }
 var reserveDefinition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.reserve_v30_archive_upload(uuid,uuid,uuid,text,text,text)'::regprocedure)`).Scan(&reserveDefinition);err!=nil{t.Fatal(err)}
 for _,fence:=range []string{"v30_archive_cross_key_begin_fence","prior.state='reserved'","prior.expected_sha=sha","v30_archive_writers","v30_archive_begin_generations","RETURN QUERY SELECT 'reconcile'"}{
  if !strings.Contains(reserveDefinition,fence){t.Fatal("cross-key Begin fence absent",fence)}
 }
 for _,signature:=range []string{"photo.start_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid)","photo.start_v30_archive_begin(uuid,uuid,uuid,uuid,uuid)"}{
  var definition string;if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef($1::regprocedure)`,signature).Scan(&definition);err!=nil{t.Fatal(err)}
  for _,fence:=range []string{"v30_archive_start_cross_key_fence","prior.expected_sha=d.expected_sha","FOR UPDATE"}{if !strings.Contains(definition,fence){t.Fatal("Start cross-key fence absent",signature,fence)}}
 }
 for _,signature:=range []string{
  "photo.claim_v30_archive_generation_cleanup()",
  "photo.v30_archive_generation_cleanup_authorized(uuid,uuid,uuid,uuid,text,text,text)",
  "photo.renew_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid,text,text,text)",
  "photo.release_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid)",
  "photo.finish_v30_archive_generation_cleanup(uuid,uuid,uuid,uuid,text,text,text)",
 }{
  var owner string;var secdef bool
  if err:=db.QueryRow(ctx,`SELECT pg_get_userbyid(p.proowner),p.prosecdef FROM pg_proc p WHERE p.oid=$1::regprocedure`,signature).Scan(&owner,&secdef);err!=nil{t.Fatal(signature,err)}
  if owner!="photo_fn_owner"||!secdef{t.Fatal("generation cleanup owner",signature)}
  for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
   var allowed bool;if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role=="photo_scheduler"){t.Fatal("generation cleanup privilege",signature,role)}
  }
 }
 for _,signature:=range []string{"photo.claim_v30_archive_cleanup()","photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text)"}{
  var definition string;if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef($1::regprocedure)`,signature).Scan(&definition);err!=nil{t.Fatal(err)}
  if !strings.Contains(definition,"v30_archive_begin_generations"){t.Fatal("parent cleanup bypasses generation",signature)}
 }
 var parentDefinition string
 if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef('photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text)'::regprocedure)`).Scan(&parentDefinition);err!=nil{t.Fatal(err)}
 if !strings.Contains(parentDefinition,"photo.v30_archive_cleanup_key_current("){t.Fatal("parent cleanup still requires legacy key")}
 var privateAllowed bool
 if err:=db.QueryRow(ctx,`SELECT has_function_privilege('photo_scheduler','photo.v30_archive_cleanup_key_current(uuid,uuid,text,text)','EXECUTE')`).Scan(&privateAllowed);err!=nil{t.Fatal(err)}
 if privateAllowed{t.Fatal("scheduler can call private cleanup key predicate")}
 for _,signature:=range []string{
  "photo.finish_tenant_deletion(uuid,uuid)",
  "photo.claim_v30_archive_cleanup()",
  "photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text)",
  "photo.v30_archive_cleanup_key_current(uuid,uuid,text,text)",
  "photo.finish_v30_archive_cleanup(uuid,uuid,uuid,text,text)",
 }{
  var definition string;if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef($1::regprocedure)`,signature).Scan(&definition);err!=nil{t.Fatal(err)}
  if !strings.Contains(definition,"purged_at IS NULL"){t.Fatal("purge path trusts terminal label without physical proof",signature)}
 }
 for _,name:=range []string{"v30_archive_aborted_requires_purge_proof","v30_archive_purge_marker_requires_aborted"}{
  var expression string
  if err:=db.QueryRow(ctx,`SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c WHERE c.conrelid='photo.v30_archive_begin_generations'::regclass AND c.conname=$1 AND c.contype='c'`,name).Scan(&expression);err!=nil{t.Fatal("generation purge constraint absent",name,err)}
  if !strings.Contains(expression,"purged_at")||!strings.Contains(expression,"aborted"){t.Fatal("generation purge constraint shape",name,expression)}
 }
}
