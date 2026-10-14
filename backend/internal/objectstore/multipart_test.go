package objectstore

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func multipartStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(Config{
		Endpoint:       "http://localhost:9000",
		PublicEndpoint: "http://localhost:9443",
		Bucket:         "photo-test",
		AccessKey:      "access",
		SecretKey:      "secret",
		Region:         "us-east-1",
		Development:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestMultipartPartURLsUsePublicOriginAndExactParts(t *testing.T) {
	store := multipartStore(t)
	key, err := StagingKey(uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	urls, err := store.SignUploadParts(context.Background(), key, "upload-token", []int{9, 1, 3}, 5*time.Minute)
	if err != nil || len(urls) != 3 {
		t.Fatal("sign parts", err, len(urls))
	}
	for i, expected := range []int{1, 3, 9} {
		parsed, parseErr := url.Parse(urls[i].URL)
		if parseErr != nil || parsed.Host != "localhost:9443" || parsed.Scheme != "http" || urls[i].PartNumber != expected || parsed.Query().Get("partNumber") != stringInt(expected) || parsed.Query().Get("uploadId") != "upload-token" {
			t.Fatal("invalid signed part", urls[i], parseErr)
		}
		if !urls[i].ExpiresAt.After(time.Now().UTC().Add(4 * time.Minute)) {
			t.Fatal("short part expiry")
		}
	}
	if _, err = store.SignUploadParts(context.Background(), key, "upload-token", []int{1, 1}, 5*time.Minute); err != ErrInvalid {
		t.Fatal("duplicate part accepted", err)
	}
	if _, err = store.SignUploadParts(context.Background(), key, "upload-token", []int{0}, 5*time.Minute); err != ErrInvalid {
		t.Fatal("part zero accepted", err)
	}
}

func TestSignedReadPinsPublicVersion(t *testing.T) {
	store := multipartStore(t)
	object := Object{Key: "tenants/test/derivatives/thumb", VersionID: "version-1", SHA256: strings.Repeat("a", 64), ContentType: "image/jpeg", Bytes: 123}
	signed, err := store.SignRead(context.Background(), object, 5*time.Minute)
	parsed, parseErr := url.Parse(signed.URL)
	if err != nil || parseErr != nil || parsed.Host != "localhost:9443" || parsed.Query().Get("versionId") != object.VersionID || parsed.Query().Get("response-content-disposition") != "inline" {
		t.Fatal("read URL is not version pinned", signed, err, parseErr)
	}
	if _, err = store.SignRead(context.Background(), Object{Key: object.Key, VersionID: "null", SHA256: object.SHA256, ContentType: object.ContentType, Bytes: object.Bytes}, 5*time.Minute); err != ErrInvalid {
		t.Fatal("unversioned read accepted", err)
	}
}

func TestSignedDownloadPinsVersionAndAttachmentName(t *testing.T) {
	store := multipartStore(t)
	object := Object{Key: "tenants/test/downloads/one", VersionID: "version-2", SHA256: strings.Repeat("b", 64), ContentType: "application/zip", Bytes: 456}
	signed, err := store.SignDownload(context.Background(), object, "photos-001.zip", 5*time.Minute)
	parsed, parseErr := url.Parse(signed.URL)
	if err != nil || parseErr != nil || parsed.Query().Get("versionId") != "version-2" || parsed.Query().Get("response-content-disposition") != `attachment; filename="photos-001.zip"` {
		t.Fatal("download URL is not pinned attachment", signed, err, parseErr)
	}
	if _, err = store.SignDownload(context.Background(), object, "../unsafe.zip", 5*time.Minute); err != ErrInvalid {
		t.Fatal("unsafe attachment accepted", err)
	}
}

func TestMultipartBoundariesRejectBeforeStorage(t *testing.T) {
	store := multipartStore(t)
	key, _ := StagingKey(uuid.New(), uuid.New())
	if _, err := store.BeginMultipart(context.Background(), "../escape", "image/jpeg"); err != ErrInvalid {
		t.Fatal("unsafe key accepted", err)
	}
	if _, err := store.CompleteMultipart(context.Background(), key, "token", "image/jpeg", MaxUploadBytes+1, []UploadedPart{{PartNumber: 1, ETag: "etag"}}); err != ErrInvalid {
		t.Fatal("oversize completion accepted", err)
	}
	if _, err := store.CompleteMultipart(context.Background(), key, "token", "image/jpeg", 1, []UploadedPart{{PartNumber: 2, ETag: "etag"}, {PartNumber: 1, ETag: "etag"}}); err != ErrInvalid {
		t.Fatal("unordered completion accepted", err)
	}
	if _, err := store.CompleteMultipart(context.Background(), key, "token", "image/jpeg", 1, []UploadedPart{{PartNumber: 1, ETag: "bad\x00etag"}}); err != ErrInvalid {
		t.Fatal("NUL-containing completion ETag accepted", err)
	}
	if err := store.AbortMultipart(context.Background(), key, "bad\nvalue"); err != ErrInvalid {
		t.Fatal("invalid upload id accepted", err)
	}
	if _, err := store.VerifyPending(context.Background(), PendingObject{Key: key, VersionID: "null", ContentType: "image/jpeg", Bytes: 1}, ""); err != ErrInvalid {
		t.Fatal("unversioned pending object accepted", err)
	}
}

func TestMultipartConfigRejectsInsecurePublicProductionOrigin(t *testing.T) {
	_, err := New(Config{Endpoint: "https://storage.internal", PublicEndpoint: "http://storage.example", Bucket: "photo-test", AccessKey: "access", SecretKey: "secret"})
	if err != ErrInvalid {
		t.Fatal("insecure public origin accepted", err)
	}
}
