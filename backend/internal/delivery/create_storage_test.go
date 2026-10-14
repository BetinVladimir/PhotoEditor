package delivery

import (
	"context"
	"errors"
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
func TestCreateStoragePublishesRecoversAndCompletes(t *testing.T) {
	for _, failure := range []error{nil, objectstore.ErrExists, objectstore.ErrUnavailable} {
		intent, session, catalog, items := createDocumentsFixture(t)
		docs, err := intent.CompileDocuments(context.Background(), session, catalog, items)
		if err != nil {
			t.Fatal(err)
		}
		doc := docs.Params
		store := &createStorageStoreStub{reservation: artifactjson.Reservation{ArtifactID: doc.Ref.ArtifactID, Token: uuid.New()}}
		objects := &createStorageObjectsStub{failure: failure}
		value, err := (CreateStoragePublisher{Tenant: intent.tenant, Store: store, Objects: objects}).PublishCreateDocument(context.Background(), intent.Identity(), doc)
		wantRecovery := 0
		if failure != nil {
			wantRecovery = 1
		}
		if err != nil || value != doc.Ref || store.reserve != 1 || store.complete != 1 || objects.put != 1 || objects.recover != wantRecovery || digest(objects.body) != doc.Ref.SHA256 {
			t.Fatal(value, err, store, objects)
		}
	}
}
func TestCreateStorageReadyReplaySkipsObjectIO(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	docs, err := intent.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	doc := docs.Params
	store := &createStorageStoreStub{reservation: artifactjson.Reservation{ArtifactID: doc.Ref.ArtifactID, Ready: &artifactjson.Ref{ID: doc.Ref.ArtifactID, SHA256: doc.Ref.SHA256}}}
	objects := &createStorageObjectsStub{}
	if _, err := (CreateStoragePublisher{Tenant: intent.tenant, Store: store, Objects: objects}).PublishCreateDocument(context.Background(), intent.Identity(), doc); err != nil || objects.put != 0 || objects.recover != 0 || store.complete != 0 {
		t.Fatal(err)
	}
}
func TestCreateStorageRefusesCorruptionAndCancellation(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	docs, err := intent.CompileDocuments(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	doc := docs.Params
	for _, cancelAfterPut := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		store := &createStorageStoreStub{reservation: artifactjson.Reservation{ArtifactID: doc.Ref.ArtifactID, Token: uuid.New()}}
		objects := &createStorageObjectsStub{corrupt: !cancelAfterPut}
		if cancelAfterPut {
			objects.cancel = cancel
		}
		_, err := (CreateStoragePublisher{Tenant: intent.tenant, Store: store, Objects: objects}).PublishCreateDocument(ctx, intent.Identity(), doc)
		cancel()
		if err == nil || store.complete != 0 {
			t.Fatal("corrupt/cancelled object committed", err)
		}
	}
	expected := errors.New("stale complete")
	store := &createStorageStoreStub{reservation: artifactjson.Reservation{ArtifactID: doc.Ref.ArtifactID, Token: uuid.New()}, err: expected}
	if _, err := (CreateStoragePublisher{Tenant: intent.tenant, Store: store, Objects: &createStorageObjectsStub{}}).PublishCreateDocument(context.Background(), intent.Identity(), doc); !errors.Is(err, expected) {
		t.Fatal(err)
	}
	store = &createStorageStoreStub{}
	doc.Body = []byte("broken")
	if _, err := (CreateStoragePublisher{Tenant: intent.tenant, Store: store, Objects: &createStorageObjectsStub{}}).PublishCreateDocument(context.Background(), intent.Identity(), doc); err == nil || store.reserve != 0 {
		t.Fatal("invalid document reached reservation")
	}
}
