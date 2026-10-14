package delivery

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type inventoryPublicationService struct {calls int;failAt int;failure error;originSessions []uuid.UUID;foreignActor bool}
func(s *inventoryPublicationService)PrepareAndPublishMetadataSource(ctx context.Context,who auth.Identity,tenant,project,parent uuid.UUID,binding v2input.SessionVersionBinding,image objectstore.Object,policy string,fields []string,deps MetadataSourceAPIDependencies)(MetadataSourcePublication,error){
 s.calls++;if s.calls==s.failAt{return MetadataSourcePublication{},s.failure}
 source,_:=canonical.Marshal(MetadataDocument{});id,err:=MetadataSourceIdentity(parent,binding,policy,fields);if err!=nil{return MetadataSourcePublication{},err}
 scope:=MetadataSourcePublicationScope{Tenant:tenant,Project:project,Actor:who.UserID,Session:who.SessionID,Binding:binding,Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("d",64)},OriginalBytes:100,OriginalMIME:"image/jpeg"}
 if len(s.originSessions)>=s.calls{scope.Session=s.originSessions[s.calls-1]}
 if s.foreignActor{scope.Actor=uuid.New()}
 return CompileMetadataSourcePublication(scope,v2input.Ref{ArtifactID:id,SHA256:digest(source)},source,policy,fields)
}
func TestPrepareMetadataInventoryCompleteAndFailure(t *testing.T){
 items:=[]v2input.SessionSnapshotItem{};images:=[]objectstore.Object{}
 for n:=0;n<2;n++{
  source:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
  items=append(items,v2input.SessionSnapshotItem{Ordinal:n,Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:source,CanvasHash:strings.Repeat("b",64)},WorkingImage:source,Recipe:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("c",64)}})
  images=append(images,objectstore.Object{Key:"working-image",VersionID:"v1",SHA256:source.SHA256,Bytes:100})
 }
 _,raw,err:=v2input.CompilePinnedSessionInputSnapshot(uuid.New(),items);if err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant,project,parent:=uuid.New(),uuid.New(),uuid.New();ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)}
 failure:=errors.New("publication failed")
 for _,mode:=range []string{"complete","partial","invalid_last_image","cancelled","mixed_sessions","foreign_actor"}{
  service:=&inventoryPublicationService{};current:=append([]objectstore.Object(nil),images...);ctx,cancel:=context.WithCancel(context.Background())
  if mode=="mixed_sessions"{service.originSessions=[]uuid.UUID{uuid.New(),uuid.New()}};if mode=="foreign_actor"{service.foreignActor=true}
  if mode=="partial"{service.failAt=2;service.failure=failure};if mode=="invalid_last_image"{current[1].VersionID="null"};if mode=="cancelled"{cancel()}
  inventory,err:=PrepareMetadataInventory(ctx,service,who,tenant,project,parent,ref,raw,current,"copyright",nil,MetadataSourceAPIDependencies{});cancel()
  if mode=="complete"||mode=="mixed_sessions"{if err!=nil||service.calls!=2{t.Fatal(mode,err)};if _,err=inventory.Entry(1,items[1].Binding);err!=nil{t.Fatal(err)}}else{
   if err==nil||inventory.SHA256()!=""{t.Fatal("incomplete inventory accepted",mode)}
   if mode=="partial"&&!errors.Is(err,failure){t.Fatal(err)}
   if mode!="partial"&&mode!="foreign_actor"&&service.calls!=0{t.Fatal("side effect before preflight",mode)}
  }
 }
}
