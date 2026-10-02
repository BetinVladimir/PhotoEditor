package objectstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoverImmutableVerifiesActualVersionBytes(t *testing.T) {
	for _, variant := range []string{"valid", "corrupt", "wrong-metadata", "unversioned", "missing"} {
		t.Run(variant, func(t *testing.T) {
			data := []byte(`{"selection":[]}`)
			hash := digest(data)
			gets := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if variant == "missing" {
					w.WriteHeader(404)
					return
				}
				version := "version-1"
				if variant == "unversioned" {
					version = "null"
				}
				w.Header().Set("X-Amz-Version-Id", version)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
				w.Header().Set("ETag", `"01234567890123456789012345678901"`)
				metadata := hash
				if variant == "wrong-metadata" {
					metadata = digest([]byte("other"))
				}
				w.Header().Set("X-Amz-Meta-Sha256", metadata)
				if r.Method == http.MethodGet {
					gets++
					if r.URL.Query().Get("versionId") != "version-1" {
						t.Error("GET did not pin version")
					}
					body := append([]byte(nil), data...)
					if variant == "corrupt" {
						body[1] = 'x'
					}
					w.Write(body)
				}
			}))
			defer server.Close()
			store, err := New(Config{Endpoint: server.URL, Bucket: "test-bucket", AccessKey: "test-access", SecretKey: "test-secret", Development: true})
			if err != nil {
				t.Fatal(err)
			}
			object, err := store.RecoverImmutable(context.Background(), "tenants/example/document", int64(len(data)), hash, "application/json")
			if variant == "valid" {
				if err != nil || object.VersionID != "version-1" || gets != 1 {
					t.Fatalf("failed recovery %+v %v GETs=%d", object, err, gets)
				}
			} else {
				if err == nil || object.Key != "" {
					t.Fatal("accepted invalid object", variant)
				}
				if variant != "missing" && !errors.Is(err, ErrIntegrity) {
					t.Fatal("unexpected integrity error", err)
				}
				if variant != "corrupt" && gets != 0 {
					t.Fatal("download after invalid metadata")
				}
			}
		})
	}
}
