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

func TestStylePackageMultipartDigestClosedNamespaceAndBounds(t *testing.T) {
	key, _ := StylePackageKey(uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"))
	for _, mime := range []string{"application/json", "application/zip"} {
		for _, n := range []int64{1, 100 << 20} {
			if !allowsMultipartDigestVerification(Object{Key: key, Bytes: n, ContentType: mime}) {
				t.Fatal("valid bounded package refused")
			}
		}
	}
	for _, n := range []int64{0, -1, (100 << 20) + 1} {
		if allowsMultipartDigestVerification(Object{Key: key, Bytes: n, ContentType: "application/json"}) {
			t.Fatal("size cap bypass")
		}
	}
	for _, bad := range []string{strings.ToUpper(key), strings.Replace(key, "aaaaaaaa", "AAAAAAAA", 1), strings.Replace(key, "bbbbbbbb", "BBBBBBBB", 1), key + "/extra", key + "-suffix", strings.Replace(key, "tenants/", "staging/tenants/", 1), "tenants/" + uuid.Nil.String() + "/style-package-uploads/" + uuid.NewString() + "/package", "tenants/" + uuid.NewString() + "/style-package-uploads/" + uuid.Nil.String() + "/package", "unrelated/object"} {
		if allowsMultipartDigestVerification(Object{Key: bad, Bytes: 10, ContentType: "application/json"}) {
			t.Fatal("noncanonical source admitted")
		}
	}
	if allowsMultipartDigestVerification(Object{Key: key, Bytes: 10, ContentType: "image/png"}) {
		t.Fatal("unsupported package media")
	}
}
func TestIntegrationStylePackageMultipartPinnedDigestBeforeOpenAndRange(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	body := []byte(`{"manual":"verified multipart bytes"}`)
	for _, mime := range []string{"application/json", "application/zip"} {
		tenant, upload := uuid.New(), uuid.New()
		key, _ := StylePackageKey(tenant, upload)
		id, e := s.BeginStylePackageMultipart(ctx, tenant, upload, mime)
		if e != nil {
			t.Fatal("begin")
		}
		parts, e := s.SignUploadParts(ctx, key, id, []int{1}, time.Minute)
		if e != nil {
			t.Fatal("sign")
		}
		req, e := http.NewRequestWithContext(ctx, "PUT", parts[0].URL, bytes.NewReader(body))
		if e != nil {
			t.Fatal("put request")
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal("put")
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			t.Fatal("put status")
		}
		pending, e := s.CompleteMultipart(ctx, key, id, mime, int64(len(body)), []UploadedPart{{PartNumber: 1, ETag: resp.Header.Get("ETag")}})
		if e != nil {
			t.Fatal("complete")
		}
		object, e := s.VerifyPending(ctx, pending, digest(body))
		if e != nil {
			t.Fatal("completion verify")
		}
		stat, e := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{VersionID: object.VersionID})
		if e != nil || stat.Metadata.Get("X-Amz-Meta-Sha256") != "" {
			t.Fatal("fixture must be real multipart without digest metadata")
		}
		reader, e := s.Open(ctx, object)
		if e != nil {
			t.Fatal("full open")
		}
		actual, e := io.ReadAll(reader)
		reader.Close()
		if e != nil || !bytes.Equal(actual, body) {
			t.Fatal("full read")
		}
		reader, e = s.OpenRange(ctx, object, 0, 4)
		if e != nil {
			t.Fatal("range open")
		}
		actual, e = io.ReadAll(reader)
		reader.Close()
		if e != nil || !bytes.Equal(actual, body[:5]) {
			t.Fatal("range read")
		}
		wrong := object
		wrong.SHA256 = strings.Repeat("0", 64)
		if reader, e = s.OpenRange(ctx, wrong, 0, 4); !errors.Is(e, ErrIntegrity) || reader != nil {
			t.Fatal("range exposed before full digest verification")
		}
		wrong = object
		wrong.Bytes++
		if !errors.Is(s.Check(ctx, wrong), ErrIntegrity) {
			t.Fatal("size mismatch")
		}
		wrong = object
		wrong.ContentType = "image/png"
		if !errors.Is(s.Check(ctx, wrong), ErrIntegrity) {
			t.Fatal("type mismatch")
		}
		wrong = object
		wrong.VersionID = uuid.NewString()
		if s.Check(ctx, wrong) == nil {
			t.Fatal("unpinned version")
		}
	}
	// Conflicting metadata never falls back, even at a canonical style key.
	key, _ := StylePackageKey(uuid.New(), uuid.New())
	info, e := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: "application/json", UserMetadata: map[string]string{"sha256": strings.Repeat("0", 64)}})
	if e != nil {
		t.Fatal("conflicting fixture")
	}
	object := Object{Key: key, VersionID: info.VersionID, SHA256: digest(body), Bytes: int64(len(body)), ContentType: "application/json"}
	if !errors.Is(s.Check(ctx, object), ErrIntegrity) {
		t.Fatal("conflicting digest metadata bypassed")
	}
	for _, test := range []struct{ key, mime string }{{key + "/extra", "application/json"}, {"unrelated/" + uuid.NewString(), "application/json"}, {func() string { k, _ := StylePackageKey(uuid.New(), uuid.New()); return k }(), "image/png"}} {
		info, e := s.client.PutObject(ctx, s.bucket, test.key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: test.mime})
		if e != nil {
			t.Fatal("negative fixture")
		}
		object := Object{Key: test.key, VersionID: info.VersionID, SHA256: digest(body), Bytes: int64(len(body)), ContentType: test.mime}
		if !errors.Is(s.Check(ctx, object), ErrIntegrity) {
			t.Fatal("unrelated or wrong media source accepted")
		}
	}
}
