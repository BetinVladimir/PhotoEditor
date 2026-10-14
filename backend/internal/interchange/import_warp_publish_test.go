package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type warpScopeStoreStub struct{preparedImportStoreStub;scope ImportPreparedScope}
func(s *warpScopeStoreStub)ReservePreparedImport(ctx context.Context,scope ImportPreparedScope,ref v2input.Ref,size int64,media string)(artifactjson.Reservation,error){s.scope=scope;return s.preparedImportStoreStub.ReservePreparedImport(ctx,scope,ref,size,media)}
func TestWarpPublisherCarriesExactOccurrenceAndRejectsWrongIdentity(t *testing.T){
 for _,invalid:=range []bool{false,true}{
  scope:=preparedPublisherScope();original:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("warp"))}
  from:=review.Canvas{Width:3,Height:2,Orientation:1,SpaceID:"source",SourceSHA256:rawSHA([]byte("original"))};to:=from;to.Width=4;to.SpaceID="intermediate"
  digest,err:=canonical.Hash(struct{From,To review.Canvas;Target v2input.SessionVersionBinding}{from,to,scope.Binding});if err!=nil{t.Fatal(err)}
  ref:=v2input.Ref{ArtifactID:uuid.NewSHA1(scope.Workflow,[]byte("interchange:warp:"+scope.Entry.String()+":"+original.ArtifactID.String()+":"+digest)),SHA256:original.SHA256};if invalid{ref.ArtifactID=uuid.New()}
  store:=&warpScopeStoreStub{preparedImportStoreStub:preparedImportStoreStub{ready:true}}
  publisher:=PreparedImportArtifactPublisher{Scope:scope,Store:store,Objects:&createStorageObjectsStub{},SourceObjects:&importReaderStub{}}
  source:=objectstore.Object{Key:"archive",VersionID:"v1",SHA256:original.SHA256,Bytes:4,ContentType:"application/octet-stream"}
  got,err:=publisher.PublishImportWarp(context.Background(),scope.Entry,ref,source,original,from,to)
  if invalid{if err==nil||store.reserve!=0{t.Fatal("wrong occurrence identity reached store")};continue}
  if err!=nil||got!=ref||store.scope.Warp==nil||store.scope.Warp.Original!=original||store.scope.Warp.From!=from||store.scope.Warp.To!=to{t.Fatal("occurrence lost",err)}
  if publisher.Scope.Warp!=nil{t.Fatal("caller publisher scope mutated")}
 }
}
