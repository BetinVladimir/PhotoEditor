package objectstore

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMultipartSourceWithoutMetadataRequiresFullPinnedByteVerification(t *testing.T) {
	for _, variant := range []string{"valid", "corrupt", "wrong-metadata", "non-source", "wrong-version", "wrong-size", "wrong-media", "oversize", "empty", "metadata-present"} {
		t.Run(variant, func(t *testing.T) {
			data := []byte("actual uploaded bytes")
			key, _ := StagingKey(uuid.New(), uuid.New())
			size := int64(len(data))
			if variant == "non-source" {
				key = "tenants/example/document"
			}
			if variant == "oversize" {
				size = MaxUploadBytes + 1
			}
			if variant == "empty" {
				size = 0
			}
			o := Object{Key: key, VersionID: "version-1", SHA256: digest(data), ContentType: "image/jpeg", Bytes: size}
			gets := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("versionId") != "version-1" {
					t.Error("unpinned storage request")
				}
				version := "version-1"
				if variant == "wrong-version" {
					version = "version-2"
				}
				media := "image/jpeg"
				if variant == "wrong-media" {
					media = "image/png"
				}
				actualSize := size
				if variant == "wrong-size" {
					actualSize++
				}
				w.Header().Set("X-Amz-Version-Id", version)
				w.Header().Set("Content-Type", media)
				w.Header().Set("Content-Length", fmt.Sprint(actualSize))
				w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
				w.Header().Set("ETag", `"01234567890123456789012345678901"`)
				if variant == "wrong-metadata" {
					w.Header().Set("X-Amz-Meta-Sha256", strings.Repeat("0", 64))
				}
				if variant == "metadata-present" {
					w.Header().Set("X-Amz-Meta-Sha256", o.SHA256)
				}
				if r.Method == http.MethodGet {
					gets++
					body := append([]byte{}, data...)
					if variant == "corrupt" {
						body[0] = 'X'
					}
					_, _ = w.Write(body)
				}
			}))
			defer server.Close()
			store, e := New(Config{Endpoint: server.URL, Bucket: "test-bucket", AccessKey: "test-access", SecretKey: "test-secret", Development: true})
			if e != nil {
				t.Fatal(e)
			}
			e = store.Check(context.Background(), o)
			if variant == "valid" {
				if e != nil || gets != 1 {
					t.Fatal("source not fully verified", e, gets)
				}
				body, err := store.Open(context.Background(), o)
				if err != nil {
					t.Fatal(err)
				}
				defer body.Close()
				got, err := io.ReadAll(body)
				if err != nil || string(got) != string(data) || gets != 3 {
					t.Fatal("source stream integrity", err, gets)
				}
			} else if variant == "metadata-present" {
				if e != nil || gets != 0 {
					t.Fatal("immutable metadata fast path changed", e, gets)
				}
			} else {
				if !errors.Is(e, ErrIntegrity) {
					t.Fatal("invalid source admitted", e)
				}
				expectedGets := 0
				if variant == "corrupt" {
					expectedGets = 1
				}
				if gets != expectedGets {
					t.Fatal("unexpected storage read", gets, expectedGets)
				}
			}
		})
	}
}

func TestMultipartSourceKeyRequiresCanonicalNonzeroUUIDs(t *testing.T) {
	key, _ := StagingKey(uuid.New(), uuid.New())
	if !isMultipartSourceKey(key) {
		t.Fatal("canonical source denied")
	}
	for _, key := range []string{"staging/tenants/00000000-0000-0000-0000-000000000000/uploads/11111111-1111-4111-8111-111111111111", "staging/tenants/not-a-uuid/uploads/not-a-uuid", key + "/extra", strings.ToUpper(key), "tenants/example/file", "../" + key} {
		if isMultipartSourceKey(key) {
			t.Fatal("noncanonical source admitted")
		}
	}
}

func TestMultipartSourceRangeValidatesWholeVersionBeforeServingPartialBytes(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) {
			data := []byte("source bytes outside the range")
			key, _ := StagingKey(uuid.New(), uuid.New())
			o := Object{Key: key, VersionID: "version-1", SHA256: digest(data), ContentType: "image/jpeg", Bytes: int64(len(data))}
			full, partial := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("versionId") != "version-1" {
					t.Error("unpinned request")
				}
				w.Header().Set("X-Amz-Version-Id", "version-1")
				w.Header().Set("Content-Type", "image/jpeg")
				w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
				w.Header().Set("ETag", `"01234567890123456789012345678901"`)
				if r.Header.Get("Range") != "" {
					partial++
					if r.Header.Get("Range") != "bytes=2-5" {
						t.Error("wrong range")
					}
					w.Header().Set("Content-Length", "4")
					w.Header().Set("Content-Range", fmt.Sprintf("bytes 2-5/%d", len(data)))
					w.WriteHeader(206)
					_, _ = w.Write(data[2:6])
					return
				}
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				if r.Method == http.MethodGet {
					full++
					body := append([]byte{}, data...)
					if corrupt {
						body[len(body)-1] = 'X'
					}
					_, _ = w.Write(body)
				}
			}))
			defer server.Close()
			store, e := New(Config{Endpoint: server.URL, Bucket: "test-bucket", AccessKey: "test-access", SecretKey: "test-secret", Development: true})
			if e != nil {
				t.Fatal(e)
			}
			body, e := store.OpenRange(context.Background(), o, 2, 5)
			if corrupt {
				if !errors.Is(e, ErrIntegrity) || body != nil || full != 1 || partial != 0 {
					t.Fatal("unverified partial bytes exposed", e, full, partial)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			defer body.Close()
			got, e := io.ReadAll(body)
			if e != nil || string(got) != string(data[2:6]) || full != 1 || partial != 1 {
				t.Fatal("range not verified/pinned", e, full, partial)
			}
		})
	}
}
