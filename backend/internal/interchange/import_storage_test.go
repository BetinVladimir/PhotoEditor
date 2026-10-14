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
type importStorageStub struct{reservation artifactjson.Reservation;reserve,complete int}
func(s *importStorageStub)ReserveImport(context.Context,ImportDocumentIdentity,ImportDocument)(artifactjson.Reservation,error){s.reserve++;return s.reservation,nil}
func(s *importStorageStub)CompleteImport(_ context.Context,_ ImportDocumentIdentity,d ImportDocument,_ artifactjson.Reservation,_ objectstore.Object)(v2input.Ref,error){s.complete++;return d.Ref,nil}
func importPublicationFixture()(ImportDocumentIdentity,ImportDocument){
 identity:=ImportDocumentIdentity{Run:uuid.New(),Workflow:uuid.New()}
 task:=uuid.NewSHA1(identity.Workflow,[]byte("v2.interchange.import:0"))
 raw:=[]byte(`{"schema_version":1}`)
 return identity,ImportDocument{Purpose:"v30_interchange_import_plan",Ref:v2input.Ref{ArtifactID:uuid.NewSHA1(task,[]byte("interchange:plan:1")),SHA256:rawSHA(raw)},Body:raw}
}
func TestImportStoragePublicationRecoveryReplay(t *testing.T){
 for _,failure:=range []error{nil,objectstore.ErrExists,objectstore.ErrUnavailable}{
  identity,doc:=importPublicationFixture();store:=&importStorageStub{reservation:artifactjson.Reservation{ArtifactID:doc.Ref.ArtifactID,Token:uuid.New()}}
  objects:=&createStorageObjectsStub{failure:failure};p:=ImportStoragePublisher{Tenant:uuid.New(),Store:store,Objects:objects}
  ref,err:=p.PublishImportDocument(context.Background(),identity,doc)
  if err!=nil||ref!=doc.Ref||store.complete!=1{t.Fatal("publication",err)}
  if failure!=nil&&objects.recover!=1{t.Fatal("missing immutable recovery")}
  store.reservation.Ready=&artifactjson.Ref{ID:doc.Ref.ArtifactID,SHA256:doc.Ref.SHA256};objects.put=0;store.complete=0
  if _,err=p.PublishImportDocument(context.Background(),identity,doc);err!=nil||objects.put!=0||store.complete!=0{t.Fatal("ready replay rewrote object",err)}
 }
}
func TestImportStorageRejectsIdentityAndObjectSubstitution(t *testing.T){
 identity,doc:=importPublicationFixture();store:=&importStorageStub{reservation:artifactjson.Reservation{ArtifactID:doc.Ref.ArtifactID,Token:uuid.New()}}
 objects:=&createStorageObjectsStub{corrupt:true};p:=ImportStoragePublisher{Tenant:uuid.New(),Store:store,Objects:objects}
 if _,err:=p.PublishImportDocument(context.Background(),identity,doc);!errors.Is(err,objectstore.ErrIntegrity)||store.complete!=0{t.Fatal("invalid object completed",err)}
 identity.Workflow=uuid.New();store.reserve=0
 if _,err:=p.PublishImportDocument(context.Background(),identity,doc);err==nil||store.reserve!=0{t.Fatal("foreign identity reached SQL")}
}
func TestImportStorageCancellationAfterUpload(t *testing.T){
 identity,doc:=importPublicationFixture();store:=&importStorageStub{reservation:artifactjson.Reservation{ArtifactID:doc.Ref.ArtifactID,Token:uuid.New()}}
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 objects:=&createStorageObjectsStub{cancel:cancel};p:=ImportStoragePublisher{Tenant:uuid.New(),Store:store,Objects:objects}
 if _,err:=p.PublishImportDocument(ctx,identity,doc);!errors.Is(err,context.Canceled)||store.complete!=0{t.Fatal("cancelled upload completed",err)}
}

func TestImportEvidenceUsesDedicatedImmutableDocumentIdentity(t *testing.T){
 identity,doc:=importPublicationFixture()
 task:=uuid.NewSHA1(identity.Workflow,[]byte("v2.interchange.import:0"))
 doc.Purpose="v30_interchange_import_evidence"
 // Body is only a storage-wire fixture; SQL independently validates full evidence.
 doc.Ref.ArtifactID=uuid.NewSHA1(task,[]byte("interchange:evidence:1"))
 store:=&importStorageStub{reservation:artifactjson.Reservation{ArtifactID:doc.Ref.ArtifactID,Token:uuid.New()}}
 objects:=&createStorageObjectsStub{}
 publisher:=ImportStoragePublisher{Tenant:uuid.New(),Store:store,Objects:objects}
 if ref,err:=publisher.PublishImportDocument(context.Background(),identity,doc);err!=nil||ref!=doc.Ref||store.complete!=1{t.Fatal("evidence publication",err)}
 doc.Ref.ArtifactID=uuid.NewSHA1(task,[]byte("interchange:plan:1"));store.reserve=0
 if _,err:=publisher.PublishImportDocument(context.Background(),identity,doc);err==nil||store.reserve!=0{t.Fatal("plan/evidence alias reached storage")}
}
