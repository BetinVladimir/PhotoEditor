package interchange

import (
 "context"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationExportDescriptorWorkerAuthorityInstallation(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn=="" {t.Skip("isolated DB migrations01558/01559/01560 required")}
 ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil {t.Fatal(err)};defer db.Close()
 signatures:=[]string{
  "photo.v30_export_descriptor_authority(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,jsonb)",
  "photo.v30_export_snapshot_authority(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)",
 }
 for _,signature:=range signatures {
  var definition string
  if err:=db.QueryRow(ctx,`SELECT pg_get_functiondef($1::regprocedure)`,signature).Scan(&definition);err!=nil {t.Fatal(err)}
  gate:="context:=photo.v30_interchange_export_ready("
  if strings.Count(definition,gate)!=2 {t.Fatal("initial and final authority gates required",signature)}
  final:=strings.LastIndex(definition,gate)
  if strings.Contains(signature,"descriptor_authority") {
   if final<strings.LastIndex(definition,"v26_render_frame_array_scope") ||
    !strings.Contains(definition,"IF allowed IS DISTINCT FROM true THEN RETURN false;END IF;") ||
    !strings.Contains(definition,"RETURN context IS NOT NULL;") {t.Fatal("descriptor successful return bypasses final gate")}
  } else {
   if final<strings.LastIndex(definition,"i.contract='InputSnapshot'") ||
    final>strings.LastIndex(definition,"RETURN reference;") {t.Fatal("snapshot successful return bypasses final gate")}
  }
  var owner string;var definer,restricted bool
  err:=db.QueryRow(ctx,`SELECT pg_get_userbyid(proowner),prosecdef,
   'search_path=pg_catalog, photo'=ANY(coalesce(proconfig,'{}'::text[]))
   OR 'search_path=pg_catalog,photo'=ANY(coalesce(proconfig,'{}'::text[]))
   FROM pg_proc WHERE oid=$1::regprocedure`,signature).Scan(&owner,&definer,&restricted)
  if err!=nil {t.Fatal(err)}
  if owner!="photo_fn_owner"||!definer||!restricted {t.Fatal("invalid function installation",signature,owner,definer,restricted)}
  var publicExecute bool
  err=db.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_proc p,
   LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
   WHERE p.oid=$1::regprocedure AND a.grantee=0 AND a.privilege_type='EXECUTE')`,signature).Scan(&publicExecute)
  if err!=nil {t.Fatal(err)};if publicExecute {t.Fatal("PUBLIC execution granted",signature)}
  for _,role:=range []string{"photo_worker","photo_api","photo_scheduler","photo_gateway","photo_portal"} {
   var allowed bool
   if err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil {t.Fatal(err)}
   if allowed!=(role=="photo_worker") {t.Fatal("unexpected authority privilege",signature,role)}
  }
 }
 for _,attempt:=range []int{0,1} {
  args:=[]any{uuid.New(),uuid.New(),uuid.New(),attempt,int64(1),uuid.New(),uuid.New(),rawSHA([]byte(`{}`)),`{}`,rawSHA([]byte("closure"))}
  var snapshot []byte
  if err:=db.QueryRow(ctx,`SELECT photo.v30_export_snapshot_authority($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,args...).Scan(&snapshot);err!=nil {t.Fatal(err)}
  if len(snapshot)!=0 {t.Fatal("absent export lease disclosed snapshot")}
  args=append(args,`{}`)
  var allowed bool
  if err:=db.QueryRow(ctx,`SELECT photo.v30_export_descriptor_authority($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)`,args...).Scan(&allowed);err!=nil {t.Fatal(err)}
  if allowed {t.Fatal("absent export lease authorized descriptor")}
 }
}
