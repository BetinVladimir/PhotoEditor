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

type signingDocumentCatalogFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, int) (objectstore.Object, error)

func (f signingDocumentCatalogFunc) LookupSigningTaskDocument(ctx context.Context, w auth.Identity, r ReadySigningExport, e CanonicalSigningTask, ref v2input.Ref, max int) (objectstore.Object, error) {
	return f(ctx, w, r, e, ref, max)
}

type signingDocumentObjectsFunc func(context.Context, objectstore.Object) (io.ReadCloser, error)

func (f signingDocumentObjectsFunc) Open(ctx context.Context, o objectstore.Object) (io.ReadCloser, error) {
	return f(ctx, o)
}

type signingDocumentStream struct {
	io.Reader
	closed     bool
	closeError error
}

func (s *signingDocumentStream) Close() error { s.closed = true; return s.closeError }

func TestBoundSigningDocumentReaderVerifiesPhysicalPinnedBytes(t *testing.T) {
	for _, mode := range []string{"valid", "hash", "short", "extra", "version", "noncanonical", "media", "metadata_hash", "close", "partial_open", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			raw := []byte(`{"a":1}`)
			if mode == "noncanonical" {
				raw = []byte(`{ "a": 1 }`)
			}
			r := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest(raw)}
			object := objectstore.Object{Key: "document", VersionID: "v1", SHA256: r.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}
			body := append([]byte(nil), raw...)
			switch mode {
			case "hash":
				body = []byte(`{"a":2}`)
			case "short":
				body = body[:2]
			case "extra":
				body = append(body, 'x')
			case "media":
				object.ContentType = "image/png"
			case "metadata_hash":
				object.SHA256 = bodyDigest([]byte(`{}`))
			}
			calls, opens := 0, 0
			catalog := signingDocumentCatalogFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2input.Ref, int) (objectstore.Object, error) {
				calls++
				current := object
				if mode == "version" && calls > 1 {
					current.VersionID = "v2"
				}
				return current, nil
			})
			stream := &signingDocumentStream{Reader: bytes.NewReader(body)}
			if mode == "close" {
				stream.closeError = errors.New("close failed")
			}
			objects := signingDocumentObjectsFunc(func(context.Context, objectstore.Object) (io.ReadCloser, error) {
				opens++
				if mode == "partial_open" {
					return stream, errors.New("open failed")
				}
				return stream, nil
			})
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := (BoundSigningTaskDocumentReader{Catalog: catalog, Objects: objects}).ReadSigningTaskDocument(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ReadySigningExport{}, CanonicalSigningTask{}, r, 1024)
			if mode == "valid" {
				if err != nil || !bytes.Equal(result, raw) || calls != 2 {
					t.Fatal("verified document refused", err)
				}
			} else if err == nil || result != nil {
				t.Fatal("invalid document exposed")
			}
			if opens > 0 && !stream.closed {
				t.Fatal("stream leaked")
			}
			if (mode == "media" || mode == "metadata_hash" || mode == "cancelled") && opens != 0 {
				t.Fatal("invalid preflight reached storage")
			}
		})
	}
}

func TestSigningSQLDocumentAdaptersPreflight(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLSigningTaskDocumentCatalog{}).LookupSigningTaskDocument(ctx, auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{}, v2input.Ref{}, 1024); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := (SQLSigningImageProducer{}).ResolveSigningImageProducer(ctx, auth.Identity{}, ReadySigningExport{}, v2input.Ref{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := (SQLSigningTaskDocumentCatalog{}).LookupSigningTaskDocument(context.Background(), auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{}, v2input.Ref{}, 1024); err == nil {
		t.Fatal("unconfigured document catalog")
	}
	if _, err := (SQLSigningImageProducer{}).ResolveSigningImageProducer(context.Background(), auth.Identity{}, ReadySigningExport{}, v2input.Ref{}); err == nil {
		t.Fatal("unconfigured producer catalog")
	}
}
