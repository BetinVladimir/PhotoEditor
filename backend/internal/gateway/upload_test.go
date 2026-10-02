package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/objectstore"
	"testing"
	"time"
)

type outputRepository struct {
	ticket                UploadTicket
	beginErr, completeErr error
	begins, completes     int
}

func (r *outputRepository) Begin(context.Context, UploadRequest) (UploadTicket, error) {
	r.begins++
	return r.ticket, r.beginErr
}
func (r *outputRepository) Complete(_ context.Context, q UploadRequest, t UploadTicket, o objectstore.Object) (ArtifactRef, error) {
	r.completes++
	return ArtifactRef{t.ArtifactID, q.SHA256}, r.completeErr
}

type outputWriter struct {
	calls        int
	err          error
	wrongVersion bool
}

func (w *outputWriter) PutImmutable(ctx context.Context, key string, body io.Reader, size int64, sha, media string) (objectstore.Object, error) {
	w.calls++
	if w.err != nil {
		return objectstore.Object{}, w.err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return objectstore.Object{}, err
	}
	if int64(len(data)) != size {
		return objectstore.Object{}, objectstore.ErrIntegrity
	}
	version := "version-1"
	if w.wrongVersion {
		version = "null"
	}
	return objectstore.Object{Key: key, VersionID: version, SHA256: sha, ContentType: media, Bytes: size}, nil
}
func TestUploadRequiresReservationAndVerifiedVersion(t *testing.T) {
	_, id, _, _ := fixture(t)
	q := UploadRequest{Identity: id, Certificate: &x509.Certificate{Raw: []byte("verified-peer")}, IdempotencyKey: uuid.New(), Role: "result", MediaType: "application/json", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Bytes: 2}
	artifact := uuid.New()
	key, _ := objectstore.AttemptKey(id.TenantID, id.TaskID, artifact, id.Attempt, id.Fence)
	repo := &outputRepository{ticket: UploadTicket{ArtifactID: artifact, UploadID: uuid.New(), ObjectKey: key}}
	writer := &outputWriter{}
	s := UploadService{Repository: repo, Objects: writer}
	ref, replay, err := s.Upload(context.Background(), q, bytes.NewBufferString("{}"))
	if err != nil || replay || ref.ArtifactID != artifact || repo.completes != 1 || writer.calls != 1 {
		t.Fatal("valid upload failed", err)
	}
	repo.beginErr = ErrUploadQuota
	if _, _, err = s.Upload(context.Background(), q, bytes.NewBufferString("{}")); !errors.Is(err, ErrUploadQuota) || writer.calls != 1 {
		t.Fatal("upload bypassed quota")
	}
	repo.beginErr = nil
	repo.ticket.ObjectKey = "other/attempt"
	if _, _, err = s.Upload(context.Background(), q, bytes.NewBufferString("{}")); !errors.Is(err, ErrUploadConflict) || writer.calls != 1 {
		t.Fatal("wrong object key accepted")
	}
	repo.ticket.ObjectKey = key
	writer.err = objectstore.ErrUnavailable
	if _, _, err = s.Upload(context.Background(), q, bytes.NewBufferString("{}")); err == nil || repo.completes != 1 {
		t.Fatal("failed storage committed")
	}
	writer.err = nil
	writer.wrongVersion = true
	if _, _, err = s.Upload(context.Background(), q, bytes.NewBufferString("{}")); !errors.Is(err, objectstore.ErrIntegrity) || repo.completes != 1 {
		t.Fatal("unversioned object committed")
	}
	writer.wrongVersion = false
	repo.completeErr = ErrCapability
	if _, _, err = s.Upload(context.Background(), q, bytes.NewBufferString("{}")); !errors.Is(err, ErrCapability) {
		t.Fatal("stale fence published")
	}
	repo.completeErr = nil
	repo.ticket.Replay = &ArtifactRef{artifact, q.SHA256}
	before := writer.calls
	ref, replay, err = s.Upload(context.Background(), q, bytes.NewBufferString("{}"))
	if err != nil || !replay || ref.ArtifactID != artifact || writer.calls != before {
		t.Fatal("replay wrote another object")
	}
}

func TestUploadHTTPBoundaries(t *testing.T) {
	signer, id, cert, now := fixture(t)
	token, err := signer.Issue(id, cert, now.Add(time.Minute), now.Add(time.Hour), []string{WriteOutputs})
	if err != nil {
		t.Fatal(err)
	}
	artifact := uuid.New()
	key, _ := objectstore.AttemptKey(id.TenantID, id.TaskID, artifact, id.Attempt, id.Fence)
	repo := &outputRepository{ticket: UploadTicket{ArtifactID: artifact, UploadID: uuid.New(), ObjectKey: key}}
	writer := &outputWriter{}
	h := HTTP{Signer: signer, MaxUploadBytes: 10, Uploads: &UploadService{Repository: repo, Objects: writer}, Tasks: resolverFunc(func(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error) {
		return TaskContext{Identity: id, Deadline: time.Now().Add(time.Minute)}, nil
	})}
	request := func() *http.Request {
		r := httptest.NewRequest("PUT", "/internal/v1/tasks/"+id.TaskID.String()+"/outputs/result", bytes.NewBufferString("{}"))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Photo-Worker", id.WorkerID.String())
		r.Header.Set("X-Photo-Attempt", "2")
		r.Header.Set("X-Photo-Fence", "9")
		r.Header.Set("Idempotency-Key", uuid.NewString())
		r.Header.Set("X-Photo-SHA256", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		r.Header.Set("Content-Type", "application/json")
		r.TLS = &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
		return r
	}
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, request())
	if w.Code != 201 || writer.calls != 1 || repo.completes != 1 {
		t.Fatal("upload handler failed", w.Code)
	}
	for _, c := range []struct {
		change func(*http.Request)
		status int
	}{
		{func(r *http.Request) { r.ContentLength = -1 }, 411},
		{func(r *http.Request) { r.ContentLength = 11 }, 413},
		{func(r *http.Request) { r.Header.Add("X-Photo-SHA256", "bad") }, 400},
		{func(r *http.Request) { r.Header.Set("Idempotency-Key", "invalid") }, 400},
		{func(r *http.Request) { r.Header.Set("Content-Type", "image/png; param=x") }, 400},
		{func(r *http.Request) { r.TLS = nil }, 403},
	} {
		r := request()
		c.change(r)
		w = httptest.NewRecorder()
		h.Handler().ServeHTTP(w, r)
		if w.Code != c.status {
			t.Fatal("unexpected refusal", w.Code, c.status)
		}
	}
	if writer.calls != 1 || repo.begins != 1 {
		t.Fatal("invalid request caused upload side effect")
	}
}

type countedUploadBody struct{ reads, largest int }

func (b *countedUploadBody) Read(p []byte) (int, error) {
	b.reads++
	if len(p) > b.largest {
		b.largest = len(p)
	}
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestUploadReaderStopsBeforeNextBlockAfterFenceChanges(t *testing.T) {
	_, id, cert, _ := fixture(t)
	calls := 0
	api := HTTP{Tasks: resolverFunc(func(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error) {
		calls++
		current := id
		if calls > 1 {
			current.Fence++
		}
		return TaskContext{Identity: current}, nil
	})}
	source := &countedUploadBody{}
	reader := &uploadReader{reader: source, controller: http.NewResponseController(httptest.NewRecorder()), ctx: context.Background(), h: &api, task: TaskContext{Identity: id}, cert: cert}
	buffer := make([]byte, 1024*1024)
	if n, err := reader.Read(buffer); err != nil || n != 64*1024 || source.largest != 64*1024 {
		t.Fatal("unbounded first block", n, err)
	}
	// Simulate the next authorization interval without sleeping.
	reader.next = time.Time{}
	if n, err := reader.Read(buffer); n != 0 || !errors.Is(err, ErrCapability) {
		t.Fatal("changed fence read bytes", n, err)
	}
	if source.reads != 1 || !errors.Is(reader.failure, ErrCapability) {
		t.Fatal("fenced body was consumed")
	}
}

func TestUploadReaderCancellationConsumesNoBody(t *testing.T) {
	source := &countedUploadBody{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &uploadReader{reader: source, ctx: ctx}
	if n, err := reader.Read(make([]byte, 1024)); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal(n, err)
	}
	if source.reads != 0 || !errors.Is(reader.failure, context.Canceled) {
		t.Fatal("cancelled body was consumed")
	}
}
