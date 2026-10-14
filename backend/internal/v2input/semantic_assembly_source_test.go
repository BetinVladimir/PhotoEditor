package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type semanticPinnedObjects struct {
	body          []byte
	calls, closed int
	last          objectstore.Object
	closeErr      error
}
type semanticPinnedReader struct {
	*bytes.Reader
	owner *semanticPinnedObjects
}

func (r *semanticPinnedReader) Close() error { r.owner.closed++; return r.owner.closeErr }
func (o *semanticPinnedObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	o.calls++
	o.last = obj
	return &semanticPinnedReader{bytes.NewReader(o.body), o}, nil
}
func TestSemanticSourceBytesStayVersionPinnedAndFailClosed(t *testing.T) {
	raw := []byte(` {"schema_version":1} `)
	hash := sha256.Sum256(raw)
	ref := Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(hash[:])}
	obj := objectstore.Object{Key: "fixture/document", VersionID: "original-v1", SHA256: ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}
	objects := &semanticPinnedObjects{body: raw}
	got, err := ReadPinnedSemanticDocument(context.Background(), objects, obj, ref)
	if err != nil || !bytes.Equal(got, raw) || objects.last.VersionID != "original-v1" || objects.closed != 1 {
		t.Fatal("lost exact raw version pin", err)
	}
	for _, body := range [][]byte{[]byte(`{"schema_version":1}`), append(append([]byte(nil), raw...), byte(' '))} {
		objects.body = body
		if _, err := ReadPinnedSemanticDocument(context.Background(), objects, obj, ref); !errors.Is(err, ErrInvalid) {
			t.Fatal("changed size/hash accepted", err)
		}
	}
	before := objects.calls
	bad := obj
	bad.VersionID = "null"
	if _, err := ReadPinnedSemanticDocument(context.Background(), objects, bad, ref); !errors.Is(err, ErrInvalid) || objects.calls != before {
		t.Fatal("unversioned read attempted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadPinnedSemanticDocument(ctx, objects, obj, ref); !errors.Is(err, context.Canceled) || objects.calls != before {
		t.Fatal("cancelled read attempted")
	}
	objects.body = raw
	objects.closeErr = io.ErrUnexpectedEOF
	if _, err := ReadPinnedSemanticDocument(context.Background(), objects, obj, ref); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("close failure ignored", err)
	}
}
