package semanticreview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type planStoreFixture struct {
	Body     []byte
	Opened   []objectstore.Object
	Closed   int
	CloseErr error
 NilStream bool
}
type planReaderFixture struct {
	io.Reader
	Store *planStoreFixture
}

func (r *planReaderFixture) Close() error { r.Store.Closed++; return r.Store.CloseErr }
func (s *planStoreFixture) Open(_ context.Context, o objectstore.Object) (io.ReadCloser, error) {
	s.Opened = append(s.Opened, o)
 if s.NilStream { return nil, nil }
	return &planReaderFixture{bytes.NewReader(s.Body), s}, nil
}

func TestPlanObjectReaderPinsVersionBoundsReadsAndCloses(t *testing.T) {
	sources, body := documentSources()
	source := sources.Objects[0]
	store := &planStoreFixture{Body: body}
	read, err := NewPlanObjectReader(store, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := read(context.Background(), source)
	if err != nil || !bytes.Equal(actual, body) || store.Closed != 1 || len(store.Opened) != 1 {
		t.Fatal(err)
	}
	opened := store.Opened[0]
	if opened.VersionID != source.Version || opened.Key != source.Key || opened.SHA256 != source.Ref.SHA256 || opened.Bytes != source.Bytes {
		t.Fatal(opened)
	}
	source.Bucket = "other"
	if _, err = read(context.Background(), source); err == nil || len(store.Opened) != 1 {
		t.Fatal("wrong bucket opened")
	}
	source.Bucket = "bucket"
	store.Body = append(append([]byte(nil), body...), byte(' '))
	if _, err = read(context.Background(), source); err == nil || store.Closed != 2 {
		t.Fatal("oversized object accepted")
	}
	store.Body = body
	failure := errors.New("close failed")
	store.CloseErr = failure
	if _, err = read(context.Background(), source); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

func TestPlanObjectReaderRejectsNilStorageStream(t *testing.T) {
 sources, _ := documentSources()
 store := &planStoreFixture{NilStream:true}
 read, err := NewPlanObjectReader(store,"bucket")
 if err != nil {t.Fatal(err)}
 body, err := read(context.Background(),sources.Objects[0])
 if !errors.Is(err,ErrInvalid) || body != nil || len(store.Opened)!=1 || store.Closed!=0 {
  t.Fatal("nil storage stream accepted",body,err)
 }
}
