package delivery

import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type metadataSourceStorageDB struct{ref v2input.Ref;token uuid.UUID;outcome string;accepted bool;calls int}
func(d *metadataSourceStorageDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 d.calls++
 if strings.Contains(q,"photo.reserve_v30_metadata_source("){
  if len(args)!=9{panic("wrong source reservation scope")}
  return metadataStorageRow{func(dest []any)error{
   *dest[0].(*string)=d.outcome
   id,token:=d.ref.ArtifactID,d.token
   *dest[1].(**uuid.UUID)=&id;*dest[2].(**uuid.UUID)=&token
   *dest[3].(*bool)=d.outcome=="replayed";return nil
  }}
 }
 if !strings.Contains(q,"photo.complete_v30_metadata_source(")||len(args)!=11{panic("wrong source completion scope")}
 return metadataStorageRow{func(dest []any)error{*dest[0].(*bool)=d.accepted;return nil}}
}
func sourceStorageFixture(t *testing.T)(MetadataSourceRepository,*metadataSourceStorageDB){
 t.Helper();author:="Artist";raw,err:=canonical.Marshal(MetadataDocument{Author:&author});if err!=nil{t.Fatal(err)}
 scope:=MetadataSourcePublicationScope{Tenant:uuid.New(),Project:uuid.New(),Actor:uuid.New(),Session:uuid.New(),
 Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)},CanvasHash:strings.Repeat("c",64)},
 Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("d",64)},OriginalBytes:100,OriginalMIME:"image/jpeg"}
 run:=uuid.New();id,err:=MetadataSourceIdentity(run,scope.Binding,"copyright",nil);if err!=nil{t.Fatal(err)}
 ref:=v2input.Ref{ArtifactID:id,SHA256:digest(raw)}
 plan,err:=CompileMetadataSourcePublication(scope,ref,raw,"copyright",nil);if err!=nil{t.Fatal(err)}
 db:=&metadataSourceStorageDB{ref:ref,token:uuid.New(),outcome:"created",accepted:true}
 return MetadataSourceRepository{db:db,scope:scope,run:run,plan:plan,policy:"copyright",approved:[]string{},bucket:"photos"},db
}
func TestMetadataSourcePublishAndReadyReplay(t *testing.T){
 repository,db:=sourceStorageFixture(t);objects:=&metadataStorageObjects{}
 ref,err:=PublishMetadataSource(context.Background(),repository,objects)
 if err!=nil||ref.ID!=db.ref.ArtifactID||ref.SHA256!=db.ref.SHA256||objects.puts!=1||db.calls!=2{t.Fatal("publication",ref,err)}
 db.outcome="replayed"
 if _,err=PublishMetadataSource(context.Background(),repository,objects);err!=nil||objects.puts!=1||db.calls!=3{t.Fatal("replay",err)}
}
func TestMetadataSourceReservationRefusals(t *testing.T){
 for outcome,expected:=range map[string]error{"quota":artifactjson.ErrQuota,"conflict":artifactjson.ErrConflict,"forbidden":artifactjson.ErrStale,"stale":artifactjson.ErrStale,"invalid":objectstore.ErrIntegrity}{
  repository,db:=sourceStorageFixture(t);db.outcome=outcome;objects:=&metadataStorageObjects{}
  if _,err:=PublishMetadataSource(context.Background(),repository,objects);err!=expected||objects.puts!=0{t.Fatal(outcome,err)}
 }
}
func TestMetadataSourceScopeAndCompletionSubstitution(t *testing.T){
 repository,db:=sourceStorageFixture(t);scope:=repository.publicationScope()
 foreign:=scope;foreign.Tenant=uuid.New()
 if _,err:=repository.Reserve(context.Background(),foreign,db.ref.SHA256,int64(len(repository.plan.SourceJCS())));err==nil||db.calls!=0{t.Fatal("scope reached SQL")}
 reservation:=artifactjson.Reservation{ArtifactID:db.ref.ArtifactID,Token:db.token}
 key,_:=objectstore.DocumentKey(scope.Tenant,scope.Run,db.ref.ArtifactID)
 good:=objectstore.Object{Key:key,SHA256:db.ref.SHA256,Bytes:int64(len(repository.plan.SourceJCS())),ContentType:"application/json",VersionID:"v1"}
 for _,mode:=range []string{"key","sha","bytes","mime","version"}{
  object:=good
  switch mode{case "key":object.Key="other";case "sha":object.SHA256=strings.Repeat("e",64);case "bytes":object.Bytes++;case "mime":object.ContentType="image/jpeg";case "version":object.VersionID="null"}
  if _,err:=repository.Complete(context.Background(),scope,reservation,object);err==nil||db.calls!=0{t.Fatal(mode)}
 }
 db.accepted=false
 if _,err:=repository.Complete(context.Background(),scope,reservation,good);err!=artifactjson.ErrStale{t.Fatal("stale completion",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel();calls:=db.calls
 if _,err:=repository.Reserve(ctx,scope,db.ref.SHA256,good.Bytes);err!=context.Canceled||db.calls!=calls{t.Fatal("cancellation",err)}
}

func TestMetadataSourceRepositoryRejectsForeignRunBeforeIO(t *testing.T){
 repository,db:=sourceStorageFixture(t)
 repository.run=uuid.New()
 objects:=&metadataStorageObjects{}
 if _,err:=PublishMetadataSource(context.Background(),repository,objects);err==nil||db.calls!=0||objects.puts!=0{
  t.Fatal("foreign deterministic identity reached publication",err,db.calls,objects.puts)
 }
}

func TestMetadataSourceRetrySessionPreservesPublication(t *testing.T){
 repository,_:=sourceStorageFixture(t);current:=uuid.New()
 rebound,err:=repository.WithCurrentSession(current);if err!=nil{t.Fatal(err)}
 if rebound.args()[3]!=current||repository.args()[3]!=repository.scope.Session||rebound.scope.Session!=repository.scope.Session||rebound.plan.SHA256()!=repository.plan.SHA256(){t.Fatal("retry rewrote immutable provenance")}
 if _,err=repository.WithCurrentSession(uuid.Nil);err==nil{t.Fatal("empty current session")}
}
