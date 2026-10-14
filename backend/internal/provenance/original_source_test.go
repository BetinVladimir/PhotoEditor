package provenance

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type signingOriginalCatalogFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, v2input.Ref) (objectstore.Object, error)

func (f signingOriginalCatalogFunc) LookupSigningOriginal(ctx context.Context, w auth.Identity, r ReadySigningExport, e CanonicalSigningTask, o, m v2input.Ref) (objectstore.Object, error) {
	return f(ctx, w, r, e, o, m)
}

type signingOriginalCancelReader struct {
	io.Reader
	cancel context.CancelFunc
}

func (r signingOriginalCancelReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.cancel()
	return n, err
}

func TestSigningOriginalQualifierHashesExactPhysicalOriginal(t *testing.T) {
	for _, mode := range []string{"valid", "hash", "short", "extra", "version", "size", "media", "metadata_hash", "close", "partial_open", "during_read", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			raw := []byte("original encoded source bytes")
			original := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(raw)}
			master := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest([]byte("working document"))}
			object := objectstore.Object{Key: "original", VersionID: "original-v1", Bytes: int64(len(raw)), SHA256: original.SHA256, ContentType: "application/octet-stream"}
			body := append([]byte(nil), raw...)
			switch mode {
			case "hash":
				body[0] = 'X'
			case "short":
				body = body[:2]
			case "extra":
				body = append(body, 'x')
			case "size":
				object.Bytes++
			case "media":
				object.ContentType = "application/json"
			case "metadata_hash":
				object.SHA256 = master.SHA256
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			stream := &signingDocumentStream{Reader: bytes.NewReader(body)}
			if mode == "close" {
				stream.closeError = errors.New("close failed")
			}
			if mode == "during_read" {
				stream.Reader = signingOriginalCancelReader{Reader: bytes.NewReader(body), cancel: cancel}
			}
			calls, opens := 0, 0
			catalog := signingOriginalCatalogFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, o, m v2input.Ref) (objectstore.Object, error) {
				calls++
				if o != original || m != master {
					return objectstore.Object{}, ErrInvalid
				}
				current := object
				if mode == "version" && calls > 1 {
					current.VersionID = "replaced-version"
				}
				return current, nil
			})
			objects := signingDocumentObjectsFunc(func(context.Context, objectstore.Object) (io.ReadCloser, error) {
				opens++
				if mode == "partial_open" {
					return stream, errors.New("open failed")
				}
				return stream, nil
			})
			err := (BoundSigningOriginalQualifier{Catalog: catalog, Objects: objects}).QualifySigningOriginal(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ReadySigningExport{}, CanonicalSigningTask{}, original, master, int64(len(raw)))
			if mode == "valid" {
				if err != nil || calls != 2 {
					t.Fatal("physical original rejected", err)
				}
			} else if err == nil {
				t.Fatal("unverified original accepted")
			}
			if opens > 0 && !stream.closed {
				t.Fatal("original stream leaked")
			}
			if (mode == "size" || mode == "media" || mode == "metadata_hash" || mode == "cancelled") && opens != 0 {
				t.Fatal("invalid original reached storage")
			}
		})
	}
}

func TestSigningOriginalSQLCatalogConfigurationAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLSigningOriginalCatalog{}).LookupSigningOriginal(ctx, auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{}, v2input.Ref{}, v2input.Ref{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := (SQLSigningOriginalCatalog{}).LookupSigningOriginal(context.Background(), auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{}, v2input.Ref{}, v2input.Ref{}); err == nil {
		t.Fatal("missing original authority accepted")
	}
}
