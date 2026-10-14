package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type approvalPreparedCatalogStub struct{calls int;failure bool}
func(s *approvalPreparedCatalogStub)LookupImportDocument(context.Context,v2input.Entry)(objectstore.Object,error){s.calls++;if s.failure{return objectstore.Object{},v2input.ErrInvalid};return objectstore.Object{Key:"prepared"},nil}
type approvalFallbackCatalogStub struct{calls int}
func(s *approvalFallbackCatalogStub)Lookup(context.Context,auth.Identity,uuid.UUID,v2draft.LayerRef)(objectstore.Object,error){s.calls++;return objectstore.Object{Key:"ordinary"},nil}
func TestImportApprovalCatalogKeepsUnacceptedDependenciesWithinSelectedScope(t *testing.T){
 tenant:=uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))}
 entries:=[]v2input.Entry{{Ref:ref,Contract:"StyleV2"}}
 prepared:=&approvalPreparedCatalogStub{};fallback:=&approvalFallbackCatalogStub{}
 catalog,err:=NewImportApprovalCatalog(tenant,who,entries,prepared,fallback);if err!=nil{t.Fatal(err)}
 entries[0].Ref.ArtifactID=uuid.New()
 object,err:=catalog.Lookup(context.Background(),who,tenant,v2draft.LayerRef{Ref:ref,Contract:"StyleV2"})
 if err!=nil||object.Key!="prepared"||prepared.calls!=1||fallback.calls!=0{t.Fatal("lost selected scope",err)}
 for _,mode:=range []string{"hash","contract","tenant","principal","session","revoked"}{t.Run(mode,func(t *testing.T){
  actor:=who;tid:=tenant;node:=v2draft.LayerRef{Ref:ref,Contract:"StyleV2"}
  switch mode{case "hash":node.Ref.SHA256=rawSHA([]byte("other"));case "contract":node.Contract="Params";case "tenant":tid=uuid.New();case "principal":actor.UserID=uuid.New();case "session":actor.SessionID=uuid.New();case "revoked":prepared.failure=true}
  if _,err:=catalog.Lookup(context.Background(),actor,tid,node);err==nil||fallback.calls!=0{t.Fatal("scope refusal fell through")}
 })}
 prepared.failure=false
 other:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("working"))}
 if object,err:=catalog.Lookup(context.Background(),who,tenant,v2draft.LayerRef{Ref:other,Contract:"WorkingImage"});err!=nil||object.Key!="ordinary"||fallback.calls!=1{t.Fatal("lost ordinary authority",err)}
 if _,err:=NewImportApprovalCatalog(tenant,who,[]v2input.Entry{{Ref:ref,Contract:"StyleV2"},{Ref:ref,Contract:"StyleV2"}},prepared,fallback);err==nil{t.Fatal("accepted duplicate scope")}
}
