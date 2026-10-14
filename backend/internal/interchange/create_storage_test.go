package interchange

import (
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type createStorageStoreStub struct {
	reservation       artifactjson.Reservation
	reserve, complete int
	err               error
}

func (s *createStorageStoreStub) ReserveCreate(_ context.Context, _ CreateIdentity, _ CreateDocument) (artifactjson.Reservation, error) {
	s.reserve++
	return s.reservation, nil
}
func (s *createStorageStoreStub) CompleteCreate(_ context.Context, _ CreateIdentity, doc CreateDocument, _ artifactjson.Reservation, _ objectstore.Object) (v2input.Ref, error) {
	s.complete++
	return doc.Ref, s.err
}

type createStorageObjectsStub struct {
	put, recover int
	failure      error
	corrupt      bool
	body         []byte
	cancel       context.CancelFunc
}

func (s *createStorageObjectsStub) PutImmutable(_ context.Context, key string, reader io.Reader, size int64, sha, mime string) (objectstore.Object, error) {
	s.put++
	s.body, _ = io.ReadAll(reader)
	if s.cancel != nil {
		s.cancel()
	}
	if s.failure != nil {
		return objectstore.Object{}, s.failure
	}
	return s.object(key, size, sha, mime), nil
}
func (s *createStorageObjectsStub) RecoverImmutable(_ context.Context, key string, size int64, sha, mime string) (objectstore.Object, error) {
	s.recover++
	return s.object(key, size, sha, mime), nil
}
func (s *createStorageObjectsStub) object(key string, size int64, sha, mime string) objectstore.Object {
	version := "version-1"
	if s.corrupt {
		version = "null"
	}
	return objectstore.Object{Key: key, Bytes: size, SHA256: sha, ContentType: mime, VersionID: version}
}
func TestInterchangeStoragePublicationAndRecovery(t *testing.T){
 for _,failure:=range []error{nil,objectstore.ErrExists,objectstore.ErrUnavailable}{
  tid,who,request,evidence,limits:=pendingFixture();plan,err:=CompileParsePlan(tid,who,request,"key",evidence,limits);if err!=nil{t.Fatal(err)}
  docs,err:=plan.Documents();if err!=nil{t.Fatal(err)};doc:=docs[0]
  store:=&createStorageStoreStub{reservation:artifactjson.Reservation{ArtifactID:doc.Ref.ArtifactID,Token:uuid.New()}}
  objects:=&createStorageObjectsStub{failure:failure}
  publisher:=CreateStoragePublisher{Tenant:tid,Store:store,Objects:objects}
  ref,err:=publisher.PublishCreateDocument(context.Background(),plan.Identity(),doc)
  if err!=nil||ref!=doc.Ref||store.reserve!=1||store.complete!=1||objects.put!=1{t.Fatal("publication failed",err)}
  expectedRecovery:=0;if failure!=nil{expectedRecovery=1};if objects.recover!=expectedRecovery{t.Fatal("lost response not recovered")}
  store.reservation.Ready=&artifactjson.Ref{ID:doc.Ref.ArtifactID,SHA256:doc.Ref.SHA256}
  objects.put=0;store.complete=0
  if _,err:=publisher.PublishCreateDocument(context.Background(),plan.Identity(),doc);err!=nil||objects.put!=0||store.complete!=0{t.Fatal("ready receipt republished",err)}
  doc.Body[0]='x';store.reserve=0
  if _,err:=publisher.PublishCreateDocument(context.Background(),plan.Identity(),doc);err==nil||store.reserve!=0{t.Fatal("altered bytes reached reservation")}
 }
}
