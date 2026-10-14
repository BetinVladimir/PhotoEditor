package interchange

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type preparedImportStoreStub struct{reserve,complete int;ready,wrong bool}
func(s *preparedImportStoreStub)ReservePreparedImport(_ context.Context,_ ImportPreparedScope,ref v2input.Ref,_ int64,_ string)(artifactjson.Reservation,error){s.reserve++;r:=artifactjson.Reservation{ArtifactID:ref.ArtifactID,Token:uuid.New()};if s.ready{r.Ready=&artifactjson.Ref{ID:ref.ArtifactID,SHA256:ref.SHA256}};if s.wrong{r.ArtifactID=uuid.New()};return r,nil}
func(s *preparedImportStoreStub)CompletePreparedImport(_ context.Context,_ ImportPreparedScope,ref v2input.Ref,_ artifactjson.Reservation,_ objectstore.Object)(v2input.Ref,error){s.complete++;return ref,nil}
func preparedPublisherScope()ImportPreparedScope{return ImportPreparedScope{Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Entry:uuid.New(),Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("master"))},CanvasHash:rawSHA([]byte("canvas"))}}}
func TestPreparedPublisherRecoversExactImmutableJSONAndReplaysWithoutIO(t *testing.T){
 for _,failure:=range []error{nil,objectstore.ErrExists,objectstore.ErrUnavailable}{
  scope:=preparedPublisherScope();raw:=[]byte(`{"value":1}`);ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}
  store:=&preparedImportStoreStub{};objects:=&createStorageObjectsStub{failure:failure}
  publisher:=PreparedImportArtifactPublisher{Scope:scope,Store:store,Objects:objects}
  result,err:=publisher.PublishImportDependency(context.Background(),scope.Entry,"Params",ref,raw)
  if err!=nil||result!=ref||store.complete!=1{t.Fatal("publish",err)}
  if failure!=nil&&objects.recover!=1{t.Fatal("recovery not used")}
  store.ready=true;objects.put=0;store.complete=0
  if _,err:=publisher.PublishImportDependency(context.Background(),scope.Entry,"Params",ref,raw);err!=nil||objects.put!=0||store.complete!=0{t.Fatal("ready replay rewrote bytes",err)}
 }
}
func TestPreparedPublisherRejectsCopiedBytesAndScopeSubstitution(t *testing.T){
 for _,change:=range []string{"none","bytes","entry","reservation","object","cancelled"}{t.Run(change,func(t *testing.T){
  scope:=preparedPublisherScope();raw:=[]byte("pinned binary content");ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}
  source:=objectstore.Object{Key:"archive-file",VersionID:"v1",ContentType:"application/octet-stream",SHA256:ref.SHA256,Bytes:int64(len(raw))}
  store:=&preparedImportStoreStub{wrong:change=="reservation"};objects:=&createStorageObjectsStub{corrupt:change=="object"};reader:=&importReaderStub{body:raw}
  if change=="bytes"{reader.body=[]byte("substituted contents!")}
  publisher:=PreparedImportArtifactPublisher{Scope:scope,Store:store,Objects:objects,SourceObjects:reader}
  entry:=scope.Entry;if change=="entry"{entry=uuid.New()}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if change=="cancelled"{objects.cancel=cancel}
  result,err:=publisher.PublishImportBinary(ctx,entry,"MaskPixels",ref,source)
  if change=="none"{if err!=nil||result!=ref||reader.opens!=1||store.complete!=1{t.Fatal("binary copy",err)};return}
  if err==nil||store.complete!=0{t.Fatal("substitution completed",err)}
  if change=="entry"&&store.reserve!=0{t.Fatal("foreign entry reached reservation")}
  if change=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
 })}
}
