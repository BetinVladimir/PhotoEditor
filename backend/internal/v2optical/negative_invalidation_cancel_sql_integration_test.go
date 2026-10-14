package v2optical

import (
 "context"
 "os"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)

// ACL evidence only; successful admission and rollback require lifecycle tests.
func TestIntegrationNegativeInvalidationCancelPrivileges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated database required")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,fn:=range []struct{signature string;private bool}{
  {"photo.request_v29_negative_invalidation_cancel(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer)",false},
  {"photo.reconcile_v29_negative_invalidation(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer)",false},
  {"photo.read_v29_negative_declined_decisions(uuid,uuid,uuid,uuid,uuid,integer)",false},
  {"photo.read_v29_negative_scheduler_decisions(uuid,integer)",false},
  {"photo.v29_negative_invalidation_authorized(uuid,uuid,uuid,uuid)",true},
  {"photo.release_v29_negative_cancelled_quota(uuid,uuid)",false},
  {"photo.release_next_v29_negative_cancelled_quota()",false},
  {"photo.read_v29_negative_selected_source(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text)",false},
  {"photo.read_v29_negative_selected_array(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)",false},
  {"photo.lock_v29_negative_selected_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb)",false},
  {"photo.read_v29_negative_frozen_intent(uuid,uuid,uuid,uuid,uuid,text,uuid)",false},
  {"photo.lock_v29_negative_replacement_scope(uuid,uuid,uuid,uuid,uuid,text,jsonb)",false},
  {"photo.lock_v29_negative_frozen_intent(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,text)",false},
  {"photo.read_v29_negative_frozen_intent_locked(uuid,uuid,uuid,uuid,uuid,text,uuid)",true},
  {"photo.close_v29_negative_skipped_dependencies(uuid,uuid,uuid,uuid,uuid,text,jsonb)",false},
  {"photo.v29_negative_skip_closure_ready(uuid,uuid)",true},
  {"photo.v29_negative_skip_task_omitted(uuid,uuid)",true},
  {"photo.read_v29_adopted_optical_parent(uuid,uuid,uuid,uuid,uuid,uuid)",false},
  {"photo.read_v29_adopted_optical_parent_array(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text)",false},
  {"photo.lock_v29_adopted_parent_proof(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb)",false},
  {"photo.read_v29_negative_affected_source_inputs(uuid,uuid,uuid,uuid,uuid,text,uuid)",false},
  {"photo.lock_v29_negative_affected_sources(uuid,uuid,uuid,uuid,uuid,text,jsonb,uuid,jsonb)",false},
  {"photo.read_v29_negative_unchanged_source(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text)",false},
  {"photo.read_v29_negative_unchanged_array(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,text,uuid,text,text)",false},
  {"photo.lock_v29_negative_unchanged_proof(uuid,uuid,uuid,uuid,uuid,text,jsonb,uuid,uuid,text,text,jsonb,jsonb)",false},
  {"photo.v29_negative_replacement_member(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,uuid)",false},
  {"photo.v29_negative_review_completion_ready_pre_skip_closure(uuid,uuid)",true},

 }{
  var secure,owned,public bool
  if err:=db.QueryRow(ctx,`SELECT p.prosecdef,r.rolname='photo_fn_owner',EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE') FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,fn.signature).Scan(&secure,&owned,&public);err!=nil{t.Fatal(err)}
  if !secure||!owned||public{t.Fatal("unsafe publication function ownership",fn.signature)}
  for _,role:=range []string{"photo_scheduler","photo_api","photo_worker","photo_gateway","photo_auth","photo_publisher","photo_maintenance","photo_portal"}{
   var allowed bool
   if err:=db.QueryRow(ctx,"SELECT has_function_privilege($1,$2,'EXECUTE')",role,fn.signature).Scan(&allowed);err!=nil{t.Fatal(err)}
   if allowed!=(role=="photo_scheduler"&&!fn.private){t.Fatal("publication authority exposed",fn.signature,role,allowed)}
  }
 }
}
