package interchange

import (
 "context"
 "os"
 "strings"
 "testing"

 "github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationSidecarImportPlanFormatScopeInstalled(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migrations through 01884 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 definitions:=[]struct{signature string;needles []string}{
  {"photo.v30_interchange_import_sources(uuid,uuid,text)",[]string{
   "source_format text", "coalesce(d.v30_interchange_proof->>'format','photoeditor_bundle')",
   "d.v30_interchange_proof->>'project_id'=w.project_id::text",
   "jsonb_build_object('format',source_format)",
  }},
  {"photo.v30_interchange_import_document_scope_pre_evidence(uuid,uuid,text,uuid,text,text)",[]string{
   "body->>'format' IS DISTINCT FROM coalesce(sources->>'format','photoeditor_bundle')",
   "entry->'label' IS DISTINCT FROM original->'label'",
   "entry->>'label' NOT IN ('Red','Yellow','Green','Blue','Purple')",
  }},
  {"photo.prepare_v30_interchange_draft_collection(uuid,uuid,text)",[]string{
   "color_label:=entry->>'label'", "i.color_label IS DISTINCT FROM color_label",
  }},
  {"photo.v30_interchange_metadata_receipts(uuid,uuid,text)",[]string{
   "jsonb_build_object('label',i.color_label)",
  }},
  {"photo.adopt_apply_results(uuid,uuid,uuid)",[]string{
   "rating,flag,color_label,tags,created_by_run_id,source_item_id)",
   "i.rating,i.flag,i.color_label,i.tags,p_run,i.id",
  }},
  {"photo.process_bulk_chunk(uuid,uuid)",[]string{
   "rating,flag,color_label,tags,source_item_id)",
   "i.moderation_state,i.rating,i.flag,i.color_label,i.tags,i.id",
  }},
 }
 var columnExists bool
 if err:=db.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM information_schema.columns
  WHERE table_schema='photo' AND table_name='collection_items'
   AND column_name='color_label' AND is_nullable='YES')`).Scan(&columnExists);err!=nil||!columnExists{
  t.Fatal("interchange color label storage missing",err)
 }
 for _,item:=range definitions{
  var definition,owner string;var security bool
  err:=db.QueryRow(ctx,`SELECT pg_get_functiondef(p.oid),pg_get_userbyid(p.proowner),p.prosecdef
    FROM pg_proc p WHERE p.oid=$1::regprocedure`,item.signature).Scan(&definition,&owner,&security)
  if err!=nil{t.Fatal(err)}
  if owner!="photo_fn_owner"||!security{t.Fatal("sidecar scope lost function owner or definer",item.signature)}
  for _,needle:=range item.needles{if !strings.Contains(definition,needle){t.Fatal("sidecar format scope not installed",item.signature,needle)}}
 }
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var source,scope bool
  err:=db.QueryRow(ctx,`SELECT has_function_privilege($1,'photo.v30_interchange_import_sources(uuid,uuid,text)','EXECUTE'),
    has_function_privilege($1,'photo.v30_interchange_import_document_scope_pre_evidence(uuid,uuid,text,uuid,text,text)','EXECUTE')`,role).Scan(&source,&scope)
  if err!=nil{t.Fatal(err)}
  if source!=(role=="photo_scheduler")||scope{t.Fatal("sidecar import scope ACL changed",role,source,scope)}
 }
}
