package gateway

import (
	"bytes"
	"context"
 "crypto/sha256"
 "encoding/hex"
	"crypto/tls"
	"crypto/x509"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/objectstore"
	"testing"
	"time"
)

type inputFunc func(context.Context, TaskIdentity, *x509.Certificate, uuid.UUID, string) (objectstore.Object, error)

func (f inputFunc) Input(c context.Context, i TaskIdentity, p *x509.Certificate, a uuid.UUID, b string) (objectstore.Object, error) {
	return f(c, i, p, a, b)
}

type memoryObjects struct {
	data  []byte
	calls int
}

func (m *memoryObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	m.calls++
	return io.NopCloser(bytes.NewReader(m.data)), nil
}
func (m *memoryObjects) OpenRange(_ context.Context, _ objectstore.Object, start, end int64) (io.ReadCloser, error) {
	m.calls++
	return io.NopCloser(bytes.NewReader(m.data[start : end+1])), nil
}
func TestDownloadAuthorizationAndRanges(t *testing.T) {
	signer, id, cert, now := fixture(t)
	artifact := uuid.New()
	objects := &memoryObjects{data: []byte("0123456789")}
	raw, err := signer.Issue(id, cert, now.Add(time.Minute), now.Add(time.Hour), []string{ReadObjects})
	if err != nil {
		t.Fatal(err)
	}
	denied := false
 version := "v1"
	h := HTTP{Signer: signer, Bucket: "private", MaxDownloadBytes: 100, Objects: objects,
		Tasks: resolverFunc(func(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error) {
			return TaskContext{Identity: id, Deadline: time.Now().Add(time.Hour)}, nil
		}),
		Inputs: inputFunc(func(_ context.Context, expected TaskIdentity, _ *x509.Certificate, requested uuid.UUID, bucket string) (objectstore.Object, error) {
			if denied || expected != id || requested != artifact || bucket != "private" {
				return objectstore.Object{}, objectstore.ErrMissing
			}
			return objectstore.Object{Key: "private/key", VersionID: version, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Bytes: 10, ContentType: "image/png"}, nil
		})}
	request := func(rangeHeader string) *http.Request {
		r := httptest.NewRequest("GET", "/internal/v1/tasks/"+id.TaskID.String()+"/objects/"+artifact.String(), nil)
		r.Header.Set("X-Photo-Worker", id.WorkerID.String())
		r.Header.Set("X-Photo-Attempt", "2")
		r.Header.Set("X-Photo-Fence", "9")
		r.Header.Set("Authorization", "Bearer "+raw)
		if rangeHeader != "" {
			r.Header.Set("Range", rangeHeader)
		}
		r.TLS = &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
		return r
	}
	for _, test := range []struct {
		header, body string
		status       int
	}{{"", "0123456789", 200}, {"bytes=2-5", "2345", 206}} {
		w := httptest.NewRecorder()
		h.Handler().ServeHTTP(w, request(test.header))
		if w.Code != test.status || w.Body.String() != test.body || w.Header().Get("X-Photo-Artifact-ID") != artifact.String() {
			t.Fatal("incorrect download", w.Code, w.Body.String())
		}
        digest := sha256.Sum256([]byte("v1"))
        if w.Header().Get("X-Photo-Media-Type") != "image/png" || w.Header().Get("X-Photo-Object-Version-SHA256") != hex.EncodeToString(digest[:]) {
            t.Fatal("immutable metadata headers missing or changed")
        }
        if w.Header().Get("Content-Type") != "application/octet-stream" {
            t.Fatal("download transport media changed")
        }
		if test.status == 206 && w.Header().Get("Content-Range") != "bytes 2-5/10" {
			t.Fatal("incorrect Content-Range")
		}
	}
	before := objects.calls
	for _, header := range []string{"bytes=1-", "bytes=-2", "bytes=0-1,4-5", "bytes=1-10", "bytes=01-2", "bytes=5-2", "bytes=0-999999999999999999999"} {
		w := httptest.NewRecorder()
		h.Handler().ServeHTTP(w, request(header))
		if w.Code != 416 {
			t.Fatal("invalid range accepted", header, w.Code)
		}
	}
	if objects.calls != before {
		t.Fatal("invalid request touched object store")
	}
	denied = true
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, request(""))
	if w.Code != 404 || objects.calls != before {
		t.Fatal("unauthorized object streamed")
	}
	denied = false
	h.MaxDownloadBytes = 2
	w = httptest.NewRecorder()
	h.Handler().ServeHTTP(w, request(""))
	if w.Code != 413 || objects.calls != before {
		t.Fatal("download quota ignored")
	}
 h.MaxDownloadBytes = 100
 for _, invalidVersion := range []string{"", "null"} {
  version = invalidVersion
  w = httptest.NewRecorder()
  h.Handler().ServeHTTP(w, request(""))
  if w.Code != 503 || objects.calls != before || w.Header().Get("X-Photo-Object-Version-SHA256") != "" {
   t.Fatal("unversioned object certified or streamed")
  }
 }
 version = "v1"
	checks := 0
	h.Tasks = resolverFunc(func(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error) {
		checks++
		if checks > 1 {
			return TaskContext{}, ErrCapability
		}
		return TaskContext{Identity: id, Deadline: time.Now().Add(time.Hour)}, nil
	})
	aborted := false
	func() {
		defer func() {
			if caught := recover(); caught != nil {
				if caught != http.ErrAbortHandler {
					panic(caught)
				}
				aborted = true
			}
		}()
		w = httptest.NewRecorder()
		h.Handler().ServeHTTP(w, request(""))
	}()
	if !aborted || w.Body.Len() != 0 {
		t.Fatal("revoked stream was not aborted before bytes")
	}

}
func TestRangeBound(t *testing.T) {
	if _, _, e := boundedRange("bytes=0-16777215", 20000000); e != nil {
		t.Fatal(e)
	}
	if _, _, e := boundedRange("bytes=0-16777216", 20000000); e == nil {
		t.Fatal("unbounded range")
	}
}
