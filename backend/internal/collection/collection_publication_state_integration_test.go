package collection

import (
 "testing"
 "github.com/google/uuid"
)

// Authored only: collection publication is independent of item publication.
func TestIntegrationCollectionOnlyPublicationSupportsEmptyGalleryAndExactCAS(t *testing.T) {
 ctx,admin,service,who,tid,id,_:=sectionAbandonFixture(t)
 var before int
 if err:=admin.QueryRow(ctx,`SELECT count(*) FROM photo.publications WHERE tenant_id=$1`,tid).Scan(&before);err!=nil {t.Fatal(err)}
 published,err:=service.Publish(ctx,who,tid,id,PublicationRequest{ExpectedRevision:1,ItemIDs:[]uuid.UUID{},Publish:true,PublishCollection:true})
 if err!=nil || published.State!="published" || published.Revision!="2" {t.Fatalf("publish empty collection: %+v %v",published,err)}
 if _,err=service.Publish(ctx,who,tid,id,PublicationRequest{ExpectedRevision:1,Publish:false,PublishCollection:true});err==nil {t.Fatal("stale publication accepted")}
 draft,err:=service.Publish(ctx,who,tid,id,PublicationRequest{ExpectedRevision:2,Publish:false,PublishCollection:true})
 if err!=nil || draft.State!="draft" || draft.Revision!="3" {t.Fatalf("unpublish: %+v %v",draft,err)}
 if _,err=service.Publish(ctx,who,tid,id,PublicationRequest{ExpectedRevision:3,Publish:true});err==nil {t.Fatal("empty item-only mutation accepted")}
 var after int
 if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.publications WHERE tenant_id=$1`,tid).Scan(&after);err!=nil || before!=after {t.Fatalf("collection publication altered photo publications: %d %d %v",before,after,err)}
}
