package objectstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoveConfirmedPinsVersionAndRequiresAbsentHEAD(t *testing.T) {
	for _, mode := range []string{"removed", "still present", "delete denied but absent", "head denied"} {
		t.Run(mode, func(t *testing.T) {
			methods := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				methods = append(methods, r.Method)
				if r.URL.Path != "/test-bucket/staging/source" || r.URL.Query().Get("versionId") != "version-to-delete" {
					t.Error("delete/verification target changed")
				}
				if r.Method == http.MethodDelete {
					if mode == "delete denied but absent" {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(403)
						fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
						return
					}
					w.WriteHeader(204)
					return
				}
				if r.Method != http.MethodHead {
					t.Error("unexpected method")
				}
				if mode == "still present" {
					w.Header().Set("Content-Length", "4")
					w.Header().Set("Content-Type", "image/jpeg")
					w.Header().Set("X-Amz-Version-Id", "version-to-delete")
					w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
					w.Header().Set("ETag", `"01234567890123456789012345678901"`)
					w.WriteHeader(200)
					return
				}
				if mode == "head denied" {
					w.WriteHeader(403)
					return
				}
				w.WriteHeader(404)
			}))
			defer server.Close()
			store, err := New(Config{Endpoint: server.URL, Bucket: "test-bucket", AccessKey: "test", SecretKey: "test", Development: true})
			if err != nil {
				t.Fatal(err)
			}
			object := Object{Key: "staging/source", VersionID: "version-to-delete", SHA256: strings.Repeat("a", 64), ContentType: "image/jpeg", Bytes: 4}
			err = store.RemoveConfirmed(context.Background(), object)
			success := mode == "removed" || mode == "delete denied but absent"
			if (err == nil) != success || len(methods) != 2 || methods[0] != http.MethodDelete || methods[1] != http.MethodHead {
				t.Fatal("unconfirmed deletion", methods, err)
			}
		})
	}
}
func TestRemoveConfirmedRejectsUnversionedObject(t *testing.T) {
	store := new(Store)
	if err := store.RemoveConfirmed(context.Background(), Object{Key: "staging/source", VersionID: "null"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid reference accepted")
	}
}
