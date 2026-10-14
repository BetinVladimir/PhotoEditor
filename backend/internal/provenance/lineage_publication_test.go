package provenance

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type signingLineageStoreStub struct {
	mode                string
	reserves, completes int
}

func (s *signingLineageStoreStub) ReadySigningLineageVersion(context.Context, auth.Identity, ReadySigningExport, v2input.Ref, []byte) (string, error) {
	if s.mode == "ready_missing_version" {
		return "", nil
	}
	return "v1", nil
}

func (s *signingLineageStoreStub) ReserveSigningLineage(_ context.Context, _ auth.Identity, _ ReadySigningExport, expected v2input.Ref, raw []byte) (artifactjson.Reservation, error) {
	s.reserves++
	value := artifactjson.Reservation{ArtifactID: expected.ArtifactID, Token: uuid.New()}
	switch s.mode {
	case "ready", "ready_missing_version":
		value.Ready = &artifactjson.Ref{ID: expected.ArtifactID, SHA256: expected.SHA256}
	case "wrong_ready":
		value.Ready = &artifactjson.Ref{ID: uuid.New(), SHA256: expected.SHA256}
	case "reservation":
		value.ArtifactID = uuid.New()
	case "token":
		value.Token = uuid.Nil
	case "mutate":
		raw[0] = 'x'
	}
	return value, nil
}
func (s *signingLineageStoreStub) CompleteSigningLineage(_ context.Context, _ auth.Identity, _ ReadySigningExport, expected v2input.Ref, _ []byte, _ artifactjson.Reservation, _ objectstore.Object) (v2input.Ref, error) {
	s.completes++
	if s.mode == "receipt" {
		expected.ArtifactID = uuid.New()
	}
	return expected, nil
}

type signingLineageObjectsStub struct {
	mode                  string
	puts, recovers, opens int
	body                  []byte
}

func (s *signingLineageObjectsStub) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	s.opens++
	if object.VersionID != "v1" || s.mode == "wrong_version" {
		return nil, objectstore.ErrMissing
	}
	if s.mode == "unavailable" {
		return nil, objectstore.ErrUnavailable
	}
	if s.mode == "bad_recover" {
		return io.NopCloser(strings.NewReader("corrupt")), nil
	}
	return io.NopCloser(bytes.NewReader(s.body)), nil
}

func (s *signingLineageObjectsStub) PutImmutable(_ context.Context, key string, reader io.Reader, size int64, sha, media string) (objectstore.Object, error) {
	s.puts++
	body, err := io.ReadAll(reader)
	if err != nil {
		return objectstore.Object{}, err
	}
	if int64(len(body)) != size || bodyDigest(body) != sha {
		return objectstore.Object{}, objectstore.ErrIntegrity
	}
	if s.mode == "exists" {
		return objectstore.Object{}, objectstore.ErrExists
	}
	if s.mode == "recover" || s.mode == "bad_recover" {
		return objectstore.Object{}, objectstore.ErrUnavailable
	}
	value := objectstore.Object{Key: key, VersionID: "v1", Bytes: size, SHA256: sha, ContentType: media}
	if s.mode == "object" {
		value.VersionID = "null"
	}
	if s.mode == "long_version" {
		value.VersionID = strings.Repeat("v", 1025)
	}
	return value, nil
}
func (s *signingLineageObjectsStub) RecoverImmutable(_ context.Context, key string, size int64, sha, media string) (objectstore.Object, error) {
	s.recovers++
	if s.mode == "unavailable" {
		return objectstore.Object{}, objectstore.ErrUnavailable
	}
	value := objectstore.Object{Key: key, VersionID: "v1", Bytes: size, SHA256: sha, ContentType: media}
	if s.mode == "bad_recover" {
		value.SHA256 = strings.Repeat("f", 64)
	}
	if s.mode == "wrong_version" || s.mode == "latest_changed" {
		value.VersionID = "v2"
	}
	return value, nil
}

func TestLineagePublisherUsesImmutableFencedProtocol(t *testing.T) {
	for _, mode := range []string{"valid", "ready", "ready_latest_changed", "ready_missing_version", "ready_bad_bytes", "exists", "recover", "mutate", "bad_recover", "object", "long_version", "reservation", "token", "wrong_ready", "receipt", "revoked", "identity", "hash", "cancelled", "write_only"} {
		t.Run(mode, func(t *testing.T) {
			body, expected := contractBytes(t, SigningLineage{Schema: 1, SourceHashes: []string{strings.Repeat("a", 64)}, Actions: []SigningAction{}})
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Run: uuid.New(), Task: uuid.New(), Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}}
			expected.ArtifactID = uuid.NewSHA1(ready.ExportID, []byte("signing:lineage:"+expected.SHA256))
			catalog := &signingCatalogStub{ready: ready, change: mode == "revoked"}
			storeMode, objectMode := mode, mode
			if mode == "ready_latest_changed" {
				storeMode, objectMode = "ready", "latest_changed"
			}
			if mode == "ready_bad_bytes" {
				storeMode, objectMode = "ready", "bad_recover"
			}
			if mode == "ready_missing_version" {
				storeMode = "ready_missing_version"
			}
			store := &signingLineageStoreStub{mode: storeMode}
			objects := &signingLineageObjectsStub{mode: objectMode, body: append([]byte(nil), body...)}
			var publicationObjects artifactjson.Objects = objects
			if mode == "write_only" {
				publicationObjects = signingWriteOnly{Objects: objects}
			}
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if mode == "identity" {
				expected.ArtifactID = uuid.New()
			}
			if mode == "hash" {
				body = []byte(`{}`)
			}
			result, err := (ImmutableSigningLineagePublisher{Catalog: catalog, Store: store, Objects: publicationObjects}).PublishSigningLineage(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ready, expected, body)
			if mode == "valid" || mode == "ready" || mode == "ready_latest_changed" || mode == "exists" || mode == "recover" || mode == "mutate" {
				if err != nil || result != expected || store.reserves != 1 {
					t.Fatal("publication failed", err)
				}
				if mode == "ready" || mode == "ready_latest_changed" {
					if objects.puts != 0 || objects.recovers != 0 || objects.opens != 1 || store.completes != 0 {
						t.Fatal("replay mutated storage")
					}
				} else if objects.puts != 1 || store.completes != 1 {
					t.Fatal("publication protocol incomplete")
				}
				if (mode == "exists" || mode == "recover") && objects.recovers != 1 {
					t.Fatal("missing immutable recovery")
				}
			} else if err == nil || result.ArtifactID != uuid.Nil {
				t.Fatal("invalid lineage publication accepted")
			}
			if (mode == "identity" || mode == "hash" || mode == "cancelled" || mode == "write_only") && (store.reserves != 0 || objects.puts != 0) {
				t.Fatal("invalid document reached reservation")
			}
			if (mode == "revoked" || mode == "object" || mode == "long_version" || mode == "bad_recover") && store.completes != 0 {
				t.Fatal("invalid evidence registered ready")
			}
		})
	}
}

func TestLineagePublisherRejectsUnboundedBodyBeforeStorage(t *testing.T) {
	for _, raw := range [][]byte{nil, make([]byte, (8<<20)+1)} {
		ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New(), Run: uuid.New(), Task: uuid.New(), Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}}
		expected := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
		store := &signingLineageStoreStub{}
		objects := &signingLineageObjectsStub{}
		result, err := (ImmutableSigningLineagePublisher{Catalog: &signingCatalogStub{ready: ready}, Store: store, Objects: objects}).PublishSigningLineage(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ready, expected, raw)
		if err != ErrInvalid || result.ArtifactID != uuid.Nil || store.reserves != 0 || store.completes != 0 || objects.puts != 0 || objects.recovers != 0 {
			t.Fatal("unbounded lineage reached storage", err)
		}
	}
}
