package objectstore

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestIntegrationMultipartSourceStreamsAndRangesAfterServerHashVerification(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key, _ := StagingKey(uuid.New(), uuid.New())
	data := []byte("actual multipart source with no client hash metadata")
	upload, e := s.BeginMultipart(ctx, key, "image/jpeg")
	if e != nil {
		t.Fatal(e)
	}
	parts, e := s.SignUploadParts(ctx, key, upload, []int{1}, 5*time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPut, parts[0].URL, bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	_, readErr := io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || readErr != nil {
		t.Fatal("part upload", res.StatusCode, readErr)
	}
	etag := strings.Trim(res.Header.Get("ETag"), `"`)
	pending, e := s.CompleteMultipart(ctx, key, upload, "image/jpeg", int64(len(data)), []UploadedPart{{PartNumber: 1, ETag: etag}})
	if e != nil {
		t.Fatal(e)
	}
	verified, e := s.VerifyPending(ctx, pending, "")
	if e != nil || verified.SHA256 != digest(data) {
		t.Fatal("server hash", e)
	}
	info, e := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{VersionID: verified.VersionID})
	if e != nil || info.Metadata.Get("X-Amz-Meta-Sha256") != "" {
		t.Fatal("fixture must exercise absent metadata", e)
	}
	body, e := s.Open(ctx, verified)
	if e != nil {
		t.Fatal(e)
	}
	got, e := io.ReadAll(body)
	body.Close()
	if e != nil || !bytes.Equal(got, data) {
		t.Fatal("verified multipart source stream", e)
	}
	ranged, e := s.OpenRange(ctx, verified, 1, 8)
	if e != nil {
		t.Fatal(e)
	}
	got, e = io.ReadAll(ranged)
	ranged.Close()
	if e != nil || !bytes.Equal(got, data[1:9]) {
		t.Fatal("verified multipart range", e)
	}
	wrong := verified
	wrong.SHA256 = strings.Repeat("0", 64)
	if e = s.Check(ctx, wrong); !errors.Is(e, ErrIntegrity) {
		t.Fatal("wrong hash accepted", e)
	}
	if body, e = s.OpenRange(ctx, wrong, 1, 8); body != nil || !errors.Is(e, ErrIntegrity) {
		if body != nil {
			body.Close()
		}
		t.Fatal("wrong full hash admitted for range", e)
	}
}
