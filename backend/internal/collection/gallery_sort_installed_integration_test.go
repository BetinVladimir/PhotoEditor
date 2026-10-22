package collection

import (
 "strings"
 "testing"
)

// This inspects actual installed definitions/ACL through a disposable migrated
// database. It is not a claim that migration or portal photo execution ran.
func TestIntegrationGallerySortInstalledPortalAndCaptureGuards(t *testing.T){
 ctx,admin,service,_,_,_,_:=sectionAbandonFixture(t)
 if err:=RequireGallerySort(ctx,service.DB);err!=nil{t.Fatal(err)}
 var definition,owner string;var secure,portal,api,worker bool
 err:=admin.QueryRow(ctx,`SELECT p.prosrc,pg_get_userbyid(p.proowner),p.prosecdef,
 has_function_privilege('photo_portal',p.oid,'EXECUTE'),has_function_privilege('photo_api',p.oid,'EXECUTE'),has_function_privilege('photo_worker',p.oid,'EXECUTE')
 FROM pg_proc p WHERE p.oid='photo.list_portal_items(bytea,uuid,bigint,uuid,integer)'::regprocedure`).Scan(&definition,&owner,&secure,&portal,&api,&worker)
 if err!=nil||owner!="photo_fn_owner"||!secure||!portal||api||worker{t.Fatalf("portal ACL: %s %v %v %v %v %v",owner,secure,portal,api,worker,err)}
 if !strings.Contains(definition,"JOIN photo.collection_item_order(c.tenant_id,c.id) ordering ON ordering.item_id=i.id")||strings.Count(definition,"ordering.sort_ordinal")!=3||strings.Contains(definition,"i.position"){t.Fatal("portal does not use effective rank in projection, cursor and order")}
 for _,raw:=range []string{"2026-10-17T13:20:30", "2026-02-30T13:20:30Z", "not a timestamp"}{
  var absent bool
  if err:=admin.QueryRow(ctx,`SELECT photo.collection_capture_instant($1) IS NULL`,raw).Scan(&absent);err!=nil||!absent{t.Fatalf("invalid absolute instant %q: %v %v",raw,absent,err)}
 }
 var equal bool
 if err:=admin.QueryRow(ctx,`SELECT photo.collection_capture_instant('2026-10-17T13:20:30+03:00')=photo.collection_capture_instant('2026-10-17T10:20:30Z')`).Scan(&equal);err!=nil||!equal{t.Fatalf("offset instant normalization: %v %v",equal,err)}
}
