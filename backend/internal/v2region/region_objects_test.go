package v2region

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type regionTestStream struct {
	io.Reader
	closed bool
}

func (s *regionTestStream) Close() error { s.closed = true; return nil }

type regionTestObjects struct {
	body   []byte
	opened objectstore.Object
	stream *regionTestStream
}

func (o *regionTestObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	o.opened = obj
	o.stream = &regionTestStream{Reader: bytes.NewReader(o.body)}
	return o.stream, nil
}
func TestRegionObjectReaderPinsBytesAndClosesStreams(t *testing.T) {
	raw := []byte(`{"example":1}`)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hash}
	obj := objectstore.Object{Key: "region-document", VersionID: "immutable-version", ContentType: "application/json", Bytes: int64(len(raw)), SHA256: hash}
	for _, bad := range []bool{false, true} {
		reader := &regionTestObjects{body: raw}
		if bad {
			reader.body = append(append([]byte{}, raw...), ' ')
		}
		body, err := readRegionDocument(context.Background(), reader, obj, ref)
		if bad && err != ErrRegionInvalid || !bad && (err != nil || !bytes.Equal(body, raw)) {
			t.Fatalf("unexpected byte proof %v", err)
		}
		if !reader.stream.closed || reader.opened.VersionID != obj.VersionID {
			t.Fatal("version/lifetime not preserved")
		}
	}
	reader := &regionTestObjects{body: raw}
	unversioned := obj
	unversioned.VersionID = ""
	if _, err := readRegionDocument(context.Background(), reader, unversioned, ref); err != ErrRegionInvalid || reader.stream != nil {
		t.Fatal("unversioned IO allowed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readRegionDocument(ctx, reader, obj, ref); err != context.Canceled || reader.stream != nil {
		t.Fatal("cancelled IO allowed")
	}
}
