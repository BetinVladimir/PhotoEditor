package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/google/uuid"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
	"time"
)

type streamObjects struct{ read func([]byte) (int, error) }

func (o streamObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	return streamBody{o.read}, nil
}
func (o streamObjects) OpenRange(context.Context, objectstore.Object, int64, int64) (io.ReadCloser, error) {
	panic("unexpected range")
}

type streamBody struct{ read func([]byte) (int, error) }

func (b streamBody) Read(p []byte) (int, error) { return b.read(p) }
func (b streamBody) Close() error               { return nil }

type failingStreamWriter struct{ *httptest.ResponseRecorder }

func (w failingStreamWriter) Write([]byte) (int, error) { return 2, errors.New("SECRET_WRITE") }

func TestDownloadStreamObservationPreservesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, stage      string
		reader           func([]byte) (int, error)
		writeFail, abort bool
	}{
		{"short_eof", "short_eof", func(p []byte) (int, error) { copy(p, "abc"); return 3, io.EOF }, false, true},
		{"read_error", "read_failed", func([]byte) (int, error) { return 0, errors.New("SECRET_READ") }, false, true},
		{"write_error", "write_failed", func(p []byte) (int, error) { copy(p, "0123456789"); return 10, io.EOF }, true, false},
		{"complete", "", func(p []byte) (int, error) { copy(p, "0123456789"); return 10, io.EOF }, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signer, id, cert, now := fixture(t)
			artifact := uuid.New()
			token, err := signer.Issue(id, cert, now.Add(time.Minute), now.Add(time.Hour), []string{ReadObjects})
			if err != nil {
				t.Fatal(err)
			}
			h := HTTP{Signer: signer, Bucket: "SECRET_BUCKET", MaxDownloadBytes: 100, Objects: streamObjects{tc.reader}, Tasks: resolverFunc(func(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error) {
				return TaskContext{Identity: id, Deadline: time.Now().Add(time.Hour)}, nil
			}), Inputs: inputFunc(func(context.Context, TaskIdentity, *x509.Certificate, uuid.UUID, string) (objectstore.Object, error) {
				return objectstore.Object{Key: "SECRET_KEY", VersionID: "SECRET_VERSION", SHA256: strings.Repeat("a", 64), Bytes: 10, ContentType: "image/png"}, nil
			})}
			r := httptest.NewRequest("GET", "/internal/v1/tasks/"+id.TaskID.String()+"/objects/"+artifact.String(), nil)
			r.Header.Set("X-Photo-Worker", id.WorkerID.String())
			r.Header.Set("X-Photo-Attempt", "2")
			r.Header.Set("X-Photo-Fence", "9")
			r.Header.Set("Authorization", "Bearer "+token)
			r.TLS = &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
			var logs bytes.Buffer
			old := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(old)
			recorder := httptest.NewRecorder()
			var writer http.ResponseWriter = recorder
			if tc.writeFail {
				writer = failingStreamWriter{recorder}
			}
			var caught any
			func() { defer func() { caught = recover() }(); h.Handler().ServeHTTP(writer, r) }()
			if tc.abort {
				if caught != http.ErrAbortHandler {
					t.Fatalf("abort changed: %T", caught)
				}
			} else if caught != nil {
				t.Fatalf("unexpected panic: %T", caught)
			}
			if strings.Contains(logs.String(), "SECRET") || strings.Contains(logs.String(), id.TaskID.String()) || strings.Contains(logs.String(), token) {
				t.Fatal("diagnostic disclosed request/error data")
			}
			if tc.writeFail && !strings.Contains(logs.String(), `"bytes_transferred":2`) {
				t.Fatal("partial written count missing")
			}
			if tc.stage == "" {
				if logs.Len() != 0 || recorder.Body.String() != "0123456789" {
					t.Fatal("normal stream changed")
				}
			} else if !strings.Contains(logs.String(), `"stage":"`+tc.stage+`"`) {
				t.Fatalf("missing static stage: %s", logs.String())
			}
		})
	}
}
func TestDownloadStreamObservationClosedVocabularyAndBudget(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	o := downloadStreamObservation{}
	o.emit("SECRET", "initial", 0, 10)
	o.emit("read_failed", "SECRET", 0, 10)
	o.emit("read_failed", "after_read", -1, 10)
	o.emit("read_failed", "after_read", 11, 10)
	if logs.Len() != 0 || o.emitted {
		t.Fatal("untrusted observation accepted")
	}
	o.emit("read_failed", "after_read", 3, 10)
	o.emit("short_eof", "eof", 3, 10)
	if strings.Count(logs.String(), "gateway stream interrupted") != 1 || strings.Contains(logs.String(), "SECRET") {
		t.Fatal("budget/filter failed")
	}
}

// This writer honors the actual ResponseController deadline, independently of
// gateway internals. The old near-expiry window truncates this healthy stream.
type deadlineStreamWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
	delay    time.Duration
}

func (w *deadlineStreamWriter) SetWriteDeadline(d time.Time) error { w.deadline = d; return nil }
func (w *deadlineStreamWriter) Write(p []byte) (int, error) {
	if !w.deadline.IsZero() && time.Until(w.deadline) < w.delay {
		time.Sleep(max(time.Until(w.deadline), 0))
		return 0, os.ErrDeadlineExceeded
	}
	time.Sleep(w.delay)
	return w.ResponseRecorder.Write(p)
}
func TestDownloadRefreshesAuthorityBeforeNearDeadlineWrite(t *testing.T) {
	signer, id, cert, now := fixture(t)
	artifact := uuid.New()
	token, err := signer.Issue(id, cert, now.Add(time.Minute), now.Add(time.Hour), []string{ReadObjects})
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	h := HTTP{Signer: signer, Bucket: "private", MaxDownloadBytes: 100,
		Objects: streamObjects{func(p []byte) (int, error) {
			time.Sleep(4500 * time.Millisecond)
			copy(p, "0123456789")
			return 10, io.EOF
		}},
		Tasks: resolverFunc(func(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error) {
			checks++
			return TaskContext{Identity: id, Deadline: time.Now().Add(time.Hour), LeaseUntil: time.Now().Add(time.Minute)}, nil
		}),
		Inputs: inputFunc(func(context.Context, TaskIdentity, *x509.Certificate, uuid.UUID, string) (objectstore.Object, error) {
			return objectstore.Object{Key: "key", VersionID: "v1", SHA256: strings.Repeat("a", 64), Bytes: 10, ContentType: "image/png"}, nil
		})}
	r := httptest.NewRequest("GET", "/internal/v1/tasks/"+id.TaskID.String()+"/objects/"+artifact.String(), nil)
	r.Header.Set("X-Photo-Worker", id.WorkerID.String())
	r.Header.Set("X-Photo-Attempt", "2")
	r.Header.Set("X-Photo-Fence", "9")
	r.Header.Set("Authorization", "Bearer "+token)
	r.TLS = &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
	w := &deadlineStreamWriter{ResponseRecorder: httptest.NewRecorder(), delay: 700 * time.Millisecond}
	h.Handler().ServeHTTP(w, r)
	if w.Body.String() != "0123456789" || checks != 3 {
		t.Fatalf("healthy stream truncated or authority not refreshed: bytes=%d checks=%d", w.Body.Len(), checks)
	}
}
