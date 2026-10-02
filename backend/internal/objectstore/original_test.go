package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type originalTestStore struct {
	source, target               Object
	putErr, recoverErr, closeErr error
	body                         []byte
	closed, recovered            bool
	opened                       Object
}
type originalTestReader struct {
	*bytes.Reader
	store *originalTestStore
}

func (r *originalTestReader) Close() error { r.store.closed = true; return r.store.closeErr }
func (s *originalTestStore) Open(_ context.Context, o Object) (io.ReadCloser, error) {
	s.opened = o
	return &originalTestReader{bytes.NewReader(s.body), s}, nil
}
func (s *originalTestStore) PutImmutable(_ context.Context, key string, r io.Reader, n int64, sha, mime string) (Object, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return Object{}, err
	}
	if !bytes.Equal(body, s.body) || n != int64(len(body)) || key != s.target.Key || sha != s.source.SHA256 || mime != s.source.ContentType {
		return Object{}, ErrInvalid
	}
	return s.target, s.putErr
}
func (s *originalTestStore) RecoverImmutable(context.Context, string, int64, string, string) (Object, error) {
	s.recovered = true
	return s.target, s.recoverErr
}
func TestCopyOriginalPreservesVersionAndRecoversUncertainWrite(t *testing.T) {
	for _, mode := range []string{"success", "exists", "uncertain", "corrupt", "recover corrupt", "close"} {
		t.Run(mode, func(t *testing.T) {
			tenant, artifact := uuid.New(), uuid.New()
			key, _ := OriginalKey(tenant, artifact)
			source := Object{Key: "staging/source", VersionID: "source-version", SHA256: strings.Repeat("a", 64), ContentType: "image/jpeg", Bytes: 4}
			target := source
			target.Key = key
			target.VersionID = "original-version"
			s := &originalTestStore{source: source, target: target, body: []byte("test")}
			switch mode {
			case "exists":
				s.putErr = ErrExists
			case "uncertain":
				s.putErr = ErrUnavailable
			case "corrupt":
				s.putErr = ErrIntegrity
			case "recover corrupt":
				s.putErr = ErrExists
				s.recoverErr = ErrIntegrity
			case "close":
				s.closeErr = errors.New("close failed")
			}
			result, err := copyOriginal(context.Background(), s, tenant, artifact, source)
			if !s.closed || s.opened != source {
				t.Fatal("source version lost or reader leaked")
			}
			success := mode == "success" || mode == "exists" || mode == "uncertain"
			if success && (err != nil || result != target) {
				t.Fatal("copy failed", err)
			}
			if !success && err == nil {
				t.Fatal("invalid original adopted")
			}
			if s.recovered != (mode == "exists" || mode == "uncertain" || mode == "recover corrupt") {
				t.Fatal("incorrect recovery policy")
			}
		})
	}
}
func TestCopyOriginalRejectsInvalidAndCancelledSource(t *testing.T) {
	tenant, artifact := uuid.New(), uuid.New()
	key, _ := OriginalKey(tenant, artifact)
	source := Object{Key: key, VersionID: "v1", SHA256: strings.Repeat("a", 64), ContentType: "image/jpeg", Bytes: 4}
	store := &originalTestStore{}
	if _, err := copyOriginal(context.Background(), store, tenant, artifact, source); !errors.Is(err, ErrInvalid) {
		t.Fatal("self copy accepted")
	}
	source.Key = "staging/source"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := copyOriginal(ctx, store, tenant, artifact, source); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored")
	}
	if store.opened.Key != "" {
		t.Fatal("invalid/cancelled copy opened source")
	}
}

func TestCopyOriginalMinIOReplayPreservesStaging(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenant, artifact := uuid.New(), uuid.New()
	body := []byte("verified original source")
	source, err := store.PutImmutable(ctx, "staging/"+uuid.NewString(), bytes.NewReader(body), int64(len(body)), digest(body), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CopyOriginal(ctx, tenant, artifact, source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CopyOriginal(ctx, tenant, artifact, source)
	if err != nil || second != first {
		t.Fatal("replay replaced original", err)
	}
	reader, err := store.Open(ctx, source)
	if err != nil {
		t.Fatal("staging removed before adoption", err)
	}
	actual, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(actual, body) {
		t.Fatal("staging changed")
	}
}
