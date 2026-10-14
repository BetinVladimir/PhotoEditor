package workflow

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"photoeditor/backend/internal/objectstore"
)

type derivativeReadStream struct {
	*bytes.Reader
	closed   bool
	closeErr error
}

func (r *derivativeReadStream) Close() error { r.closed = true; return r.closeErr }

type derivativeReadStore struct {
	stream *derivativeReadStream
	seen   objectstore.Object
	cancel context.CancelFunc
}

func (s *derivativeReadStore) Open(_ context.Context, o objectstore.Object) (io.ReadCloser, error) {
	s.seen = o
	if s.cancel != nil {
		s.cancel()
	}
	return s.stream, nil
}

func TestDerivativeReadPinsVersionAndRejectsCorruption(t *testing.T) {
	canonical := []byte(`{"schema_version":1}`)
	meta := objectstore.Object{Key: "tenants/scoped/source", VersionID: "immutable-v1", Bytes: int64(len(canonical)), SHA256: renderDerivativeBytesSHA(canonical), ContentType: "application/json"}
	for _, test := range []struct {
		name     string
		body     []byte
		closeErr error
		cancel   bool
		want     error
	}{
		{"valid", canonical, nil, false, nil},
		{"truncated", canonical[:len(canonical)-1], nil, false, objectstore.ErrIntegrity},
		{"trailing", append(append([]byte{}, canonical...), ' '), nil, false, objectstore.ErrIntegrity},
		{"substitution", []byte(`{"schema_version":2}`), nil, false, objectstore.ErrIntegrity},
		{"close-failure", canonical, io.ErrUnexpectedEOF, false, io.ErrUnexpectedEOF},
		{"cancelled-read", canonical, nil, true, context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &derivativeReadStore{stream: &derivativeReadStream{Reader: bytes.NewReader(test.body), closeErr: test.closeErr}}
			if test.cancel {
				store.cancel = cancel
			}
			body, err := readRenderDerivativeJSON(ctx, store, meta)
			if !errors.Is(err, test.want) {
				t.Fatal(err)
			}
			if !store.stream.closed || store.seen != meta {
				t.Fatal("stream not closed or immutable metadata replaced")
			}
			if test.want != nil && body != nil {
				t.Fatal("invalid body escaped")
			}
			if test.want == nil && !bytes.Equal(body, canonical) {
				t.Fatal("body changed")
			}
		})
	}
}
