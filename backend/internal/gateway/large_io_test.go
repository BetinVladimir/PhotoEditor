package gateway

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net/http/httptest"
	"os"
	"photoeditor/backend/internal/objectstore"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
)

type gatewayLargeBody struct {
	remaining int64
	block     []byte
	offset    int
}

func (r *gatewayLargeBody) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	written := 0
	for written < n {
		k := copy(p[written:n], r.block[r.offset:])
		written += k
		r.offset = (r.offset + k) % len(r.block)
	}
	r.remaining -= int64(n)
	return n, nil
}

// Real S3 IO through the production handler/service; DB and verified TLS peer
// evidence are synthetic. Full wire mTLS/SQL quota qualification is separate.
func TestIntegrationLargeOutputHandler(t *testing.T) {
	if os.Getenv("PHOTO_RUN_GATEWAY_LARGE_IO") != "1" {
		t.Skip("opt-in >4GiB output handler IO")
	}
	const size int64 = 1<<32 + 1<<20 + 17
	store, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"),
		Bucket: "gateway-large-" + uuid.NewString(), AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"),
		SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err = store.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	block := make([]byte, 1<<20)
	for i := range block {
		block[i] = byte(i % 251)
	}
	hash := sha256.New()
	for remaining := size; remaining > 0; {
		n := int64(len(block))
		if n > remaining {
			n = remaining
		}
		hash.Write(block[:n])
		remaining -= n
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	signer, id, cert, now := fixture(t)
	token, err := signer.Issue(id, cert, now.Add(time.Minute), now.Add(time.Hour), []string{WriteOutputs})
	if err != nil {
		t.Fatal(err)
	}
	artifact := uuid.New()
	key, _ := objectstore.AttemptKey(id.TenantID, id.TaskID, artifact, id.Attempt, id.Fence)
	repo := &outputRepository{ticket: UploadTicket{ArtifactID: artifact, UploadID: uuid.New(), ObjectKey: key}}
	resolves := 0
	api := HTTP{Signer: signer, MaxUploadBytes: 6 << 30, Uploads: &UploadService{Repository: repo, Objects: store},
		Tasks: resolverFunc(func(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error) {
			resolves++
			return TaskContext{Identity: id, Deadline: time.Now().Add(5 * time.Minute)}, nil
		})}
	body := &gatewayLargeBody{remaining: size, block: block}
	request := httptest.NewRequest("PUT", "/internal/v1/tasks/"+id.TaskID.String()+"/outputs/archive", body)
	request.ContentLength = size
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Photo-Worker", id.WorkerID.String())
	request.Header.Set("X-Photo-Attempt", strconv.Itoa(id.Attempt))
	request.Header.Set("X-Photo-Fence", strconv.FormatInt(id.Fence, 10))
	request.Header.Set("Idempotency-Key", uuid.NewString())
	request.Header.Set("X-Photo-SHA256", digest)
	request.Header.Set("Content-Type", "application/zip")
	request.TLS = &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
	response := httptest.NewRecorder()
	t.Logf("handler upload %d bytes", size)
	api.Handler().ServeHTTP(response, request)
	if response.Code != 201 || repo.begins != 1 || repo.completes != 1 || body.remaining != 0 || resolves < 2 {
		t.Fatalf("status=%d begin=%d complete=%d unread=%d resolves=%d", response.Code, repo.begins, repo.completes, body.remaining, resolves)
	}
	t.Logf("handler accepted %d bytes; live authorization checks=%d", size, resolves)
	if err = store.PurgeAttempt(ctx, id.TenantID, id.TaskID, artifact, id.Attempt, id.Fence); err != nil {
		t.Fatal(err)
	}
}
