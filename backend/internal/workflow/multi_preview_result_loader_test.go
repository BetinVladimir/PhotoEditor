package workflow

import (
	"bytes"
	"context"
	"io"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type multiEncodedOpener struct {
	body   []byte
	closed bool
}
type multiEncodedReader struct {
	*bytes.Reader
	owner *multiEncodedOpener
}

func (r *multiEncodedReader) Close() error { r.owner.closed = true; return nil }
func (o *multiEncodedOpener) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	return &multiEncodedReader{bytes.NewReader(o.body), o}, nil
}
func TestMultiPreviewEncodedExactHashLengthAndClose(t *testing.T) {
	body := []byte("hash proof is not encoded format acceptance")
	obj := objectstore.Object{Key: "preview", VersionID: "v1", Bytes: int64(len(body)), SHA256: renderDerivativeBytesSHA(body), ContentType: "image/webp"}
	for _, mode := range []string{"exact", "short", "long", "sha"} {
		t.Run(mode, func(t *testing.T) {
			o := &multiEncodedOpener{body: append([]byte(nil), body...)}
			expected := obj
			switch mode {
			case "short":
				o.body = o.body[:len(o.body)-1]
			case "long":
				o.body = append(o.body, 'x')
			case "sha":
				o.body[0] = 'X'
			}
			err := verifyMultiPreviewEncodedHash(context.Background(), o, expected)
			if (err == nil) != (mode == "exact") || !o.closed {
				t.Fatal("hash/length/close invariant", err, o.closed)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := &multiEncodedOpener{body: body}
	if err := verifyMultiPreviewEncodedHash(ctx, o, obj); err != context.Canceled || o.closed {
		t.Fatal("cancelled read performed IO", err)
	}
}
