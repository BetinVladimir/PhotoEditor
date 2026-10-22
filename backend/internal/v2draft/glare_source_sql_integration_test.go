package v2draft

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// This scenario is authored for the later isolated migrated database phase.
// No production database or migrations are started by this test.
func TestIntegrationGlareSourceAuthorityIsolation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database through02480 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgx.Connect(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close(context.WithoutCancel(ctx))
 cases:=[]struct{signature string;role string;guards []string}{
  {"photo.v28_glare_generative_source_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text)","photo_worker",[]string{"lock_v28_glare_source_session","v28_glare_source_confirmation","v28_glare_source_issuer_context","generative","glasses_mask"}},
  {"photo.read_v28_glare_source_admission_ticket(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid)","photo_scheduler",[]string{"lock_v28_glare_source_session","v2_input_closure_ready","v28_restoration_inventory_digest_ready","v28_glare_source_confirmation","v28_manual_pair_origin"}},
  {"photo.begin_v28_glare_source_draft(text,text,text,bigint)","photo_scheduler",[]string{"generative","donor_observation IS NOT NULL","donor_asset_version_id","null"}},
 }
 for _,c:=range cases{var definition string;var allowed,public bool
  err=db.QueryRow(ctx,`SELECT pg_get_functiondef(p.oid),has_function_privilege($2,p.oid,'EXECUTE'),EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') FROM pg_proc p WHERE p.oid=$1::regprocedure`,c.signature,c.role).Scan(&definition,&allowed,&public)
  if err!=nil{t.Fatal(err)};if !allowed||public{t.Fatal("source-only ACL drift",c.signature)}
  for _,guard:=range c.guards{if !strings.Contains(definition,guard){t.Fatal("missing source authority guard",c.signature,guard)}}
  if strings.Contains(definition,"lock_v28_donor_session("){t.Fatal("source-only function retained donor authority",c.signature)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(context.WithoutCancel(ctx))
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_scheduler`);err!=nil{t.Fatal(err)}
 tenant:=uuid.New();if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenant.String());err!=nil{t.Fatal(err)}
 var proof []byte
 err=tx.QueryRow(ctx,`SELECT photo.build_v28_glare_source_confirmation($1,$2,$3,$4,$5,$6::jsonb,$7)`,tenant,uuid.New(),uuid.New(),uuid.New(),uuid.New(),`{"mode":"generative","donor":null,"donor_faces":null}`,uuid.New()).Scan(&proof)
 if err!=nil{t.Fatal(err)};if len(proof)!=0{t.Fatal("unknown actor/source issued generative authority")}
}
