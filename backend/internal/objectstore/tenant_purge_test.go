package objectstore

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"strings"
	"testing"
	"time"
)

func TestTenantPurgeSharedBudgetAllNamespacesAndNeighbor(t *testing.T) {
	tenant, neighbor := uuid.New(), uuid.New()
	prefixes, _ := tenantNamespaces(tenant)
	objects := map[string]string{}
	uploads := map[string]bool{}
	for _, prefix := range prefixes {
		objects[prefix+"object"] = "v1"
		uploads[prefix+"upload"] = true
	}
	neighborKey := "staging/tenants/" + neighbor.String() + "/keep"
	objects[neighborKey] = "v2"
	removed := 0
	io := tenantPurgeIO{
		versions: func(_ context.Context, prefix string) <-chan minio.ObjectInfo {
			rows := make(chan minio.ObjectInfo, len(objects))
			for key, version := range objects {
				if strings.HasPrefix(key, prefix) {
					rows <- minio.ObjectInfo{Key: key, VersionID: version}
				}
			}
			close(rows)
			return rows
		},
		uploads: func(_ context.Context, prefix string) <-chan minio.ObjectMultipartInfo {
			rows := make(chan minio.ObjectMultipartInfo, len(uploads))
			for key := range uploads {
				if strings.HasPrefix(key, prefix) {
					rows <- minio.ObjectMultipartInfo{Key: key, UploadID: "upload-1"}
				}
			}
			close(rows)
			return rows
		},
		removeVersion: func(_ context.Context, key, version string) error {
			if objects[key] != version {
				t.Fatal("wrong immutable version")
			}
			delete(objects, key)
			removed++
			return nil
		},
		removeUpload: func(_ context.Context, key, uploadID string) error {
			if uploadID != "upload-1" {
				t.Fatal("wrong multipart session")
			}
			delete(uploads, key)
			removed++
			return nil
		},
	}
	passes := 0
	for {
		before := removed
		more, err := purgeTenantBatch(context.Background(), tenant, 2, io)
		if err != nil {
			t.Fatal(err)
		}
		if removed-before > 2 {
			t.Fatal("per-prefix budget reset")
		}
		passes++
		if !more {
			break
		}
		if passes > 4 {
			t.Fatal("did not converge")
		}
	}
	if removed != 6 || len(uploads) != 0 || len(objects) != 1 || objects[neighborKey] != "v2" {
		t.Fatal("incomplete or foreign deletion")
	}
}

func TestTenantPurgeRemovesOpaqueLegacyKeysWithinExactNamespace(t *testing.T) {
	tenant, neighbor := uuid.New(), uuid.New()
	prefixes, err := tenantNamespaces(tenant)
	if err != nil {
		t.Fatal(err)
	}
	objects := map[string]string{
		prefixes[0] + "old file ☃.jpg":                     "version-1",
		prefixes[2] + "legacy/../raw image":                "version-2",
        prefixes[1] + "before-versioning.raw":             "null",
		"tenants/" + neighbor.String() + "/old file ☃.jpg": "foreign",
	}
	uploads := map[string]string{prefixes[1] + "stale multipart +": "upload-1"}
	io := tenantPurgeIO{
		versions: func(_ context.Context, prefix string) <-chan minio.ObjectInfo {
			rows := make(chan minio.ObjectInfo, len(objects))
			for key, version := range objects {
				if strings.HasPrefix(key, prefix) {
					rows <- minio.ObjectInfo{Key: key, VersionID: version}
				}
			}
			close(rows)
			return rows
		},
		uploads: func(_ context.Context, prefix string) <-chan minio.ObjectMultipartInfo {
			rows := make(chan minio.ObjectMultipartInfo, len(uploads))
			for key, id := range uploads {
				if strings.HasPrefix(key, prefix) {
					rows <- minio.ObjectMultipartInfo{Key: key, UploadID: id}
				}
			}
			close(rows)
			return rows
		},
		removeVersion: func(_ context.Context, key, version string) error {
			if objects[key] != version {
				t.Fatal("wrong version deleted", key, version)
			}
			delete(objects, key)
			return nil
		},
		removeUpload: func(_ context.Context, key, id string) error {
			if uploads[key] != id {
				t.Fatal("wrong multipart deleted", key, id)
			}
			delete(uploads, key)
			return nil
		},
	}
	for pass := 0; pass < 4; pass++ {
		more, err := purgeTenantBatch(context.Background(), tenant, 2, io)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			if len(uploads) != 0 || len(objects) != 1 || objects["tenants/"+neighbor.String()+"/old file ☃.jpg"] != "foreign" {
				t.Fatal("opaque tenant key left or neighbor deleted", objects, uploads)
			}
			return
		}
	}
	t.Fatal("opaque tenant purge did not converge")
}

func TestTenantPurgeNamespacesCoverCurrentKeyGenerators(t *testing.T) {
	tenant, neighbor := uuid.New(), uuid.New()
	artifact, upload, generation, task, run := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	prefixes, err := tenantNamespaces(tenant)
	if err != nil || len(prefixes) != 3 {
		t.Fatal("tenant purge namespace inventory invalid", err)
	}
	generated := []func(uuid.UUID) (string, error){
		func(id uuid.UUID) (string, error) { return OriginalKey(id, artifact) },
		func(id uuid.UUID) (string, error) { return StagingKey(id, upload) },
		func(id uuid.UUID) (string, error) { return AttemptKey(id, task, artifact, 1, 1) },
		func(id uuid.UUID) (string, error) { return DocumentKey(id, run, artifact) },
		func(id uuid.UUID) (string, error) { return V2DraftKey(id, artifact) },
		func(id uuid.UUID) (string, error) { return InterchangeUploadKey(id, upload) },
		func(id uuid.UUID) (string, error) { return InterchangeGenerationKey(id, upload, generation) },
	}
	for _, key := range generated {
		owned, err := key(tenant)
		if err != nil {
			t.Fatal(err)
		}
		covered := false
		for _, prefix := range prefixes {
			covered = covered || strings.HasPrefix(owned, prefix)
		}
		if !covered {
			t.Fatal("tenant object key escapes physical purge", owned)
		}
		foreign, err := key(neighbor)
		if err != nil {
			t.Fatal(err)
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(foreign, prefix) {
				t.Fatal("neighbor key shares tenant purge prefix", foreign, prefix)
			}
		}
	}
}

func TestTenantPurgeRefusalsAndSecondPass(t *testing.T) {
	for _, mode := range []string{"foreign_key", "missing_version", "control_version", "oversized_version", "listing_error", "delete_error", "late_object", "cancelled", "nil_stream"} {
		t.Run(mode, func(t *testing.T) {
			tenant := uuid.New()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			calls, deletes := 0, 0
			io := tenantPurgeIO{
				versions: func(_ context.Context, prefix string) <-chan minio.ObjectInfo {
					calls++
					if mode == "nil_stream" {
						return nil
					}
					rows := make(chan minio.ObjectInfo, 1)
					row := minio.ObjectInfo{Key: prefix + "object", VersionID: "v1"}
					switch mode {
					case "foreign_key":
						row.Key = "tenants/" + uuid.NewString() + "/foreign"
					case "missing_version":
						row.VersionID = ""
					case "control_version":
						row.VersionID = "v1\n"
					case "oversized_version":
						row.VersionID = strings.Repeat("v", 1025)
					case "listing_error":
						row.Err = errors.New("listing failed")
					}
					if mode != "late_object" || calls == 4 {
						rows <- row
					}
					close(rows)
					return rows
				},
				uploads: func(context.Context, string) <-chan minio.ObjectMultipartInfo {
					rows := make(chan minio.ObjectMultipartInfo)
					close(rows)
					return rows
				},
				removeVersion: func(context.Context, string, string) error { deletes++; return errors.New("delete failed") },
				removeUpload:  func(context.Context, string, string) error { t.Fatal("unexpected upload"); return nil },
			}
			more, err := purgeTenantBatch(ctx, tenant, 10, io)
			if mode == "late_object" {
				if err != nil || !more || deletes != 0 {
					t.Fatal("late remnant completed purge")
				}
			} else if err == nil {
				t.Fatal("failure reported success")
			}
			if mode != "delete_error" && deletes != 0 {
				t.Fatal("deleted invalid or unverified object")
			}
		})
	}
}

func TestTenantPurgeCancellationInterruptsStalledListing(t *testing.T) {
	for _, stalled := range []string{"uploads", "versions"} {
		tenant := uuid.New()
		ctx, cancel := context.WithCancel(context.Background())
		entered := make(chan struct{})
		done := make(chan error, 1)
		io := tenantPurgeIO{
			uploads: func(context.Context, string) <-chan minio.ObjectMultipartInfo {
				if stalled == "uploads" {
					close(entered)
					return make(chan minio.ObjectMultipartInfo)
				}
				rows := make(chan minio.ObjectMultipartInfo)
				close(rows)
				return rows
			},
			versions: func(context.Context, string) <-chan minio.ObjectInfo {
				if stalled == "versions" {
					close(entered)
					return make(chan minio.ObjectInfo)
				}
				rows := make(chan minio.ObjectInfo)
				close(rows)
				return rows
			},
			removeUpload:  func(context.Context, string, string) error { t.Fatal("unexpected deletion"); return nil },
			removeVersion: func(context.Context, string, string) error { t.Fatal("unexpected deletion"); return nil },
		}
		go func() { _, err := purgeTenantBatch(ctx, tenant, 8, io); done <- err }()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("listing not entered", stalled)
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("wrong cancellation", stalled, err)
			}
		case <-time.After(time.Second):
			t.Fatal("stalled listing ignored cancellation", stalled)
		}
	}
}

func TestTenantPurgeCountsEachMultipartSessionAgainstBatchLimit(t *testing.T) {
	tenant := uuid.New()
	prefixes, _ := tenantNamespaces(tenant)
	key := prefixes[0] + "shared-key"
	pending := map[string]bool{"upload-a": true, "upload-b": true}
	removed := []string{}
	io := tenantPurgeIO{
		uploads: func(_ context.Context, prefix string) <-chan minio.ObjectMultipartInfo {
			rows := make(chan minio.ObjectMultipartInfo, 2)
			if prefix == prefixes[0] {
				for id := range pending {
					rows <- minio.ObjectMultipartInfo{Key: key, UploadID: id}
				}
			}
			close(rows)
			return rows
		},
		versions: func(context.Context, string) <-chan minio.ObjectInfo {
			rows := make(chan minio.ObjectInfo)
			close(rows)
			return rows
		},
		removeUpload: func(_ context.Context, gotKey, id string) error {
			if gotKey != key || !pending[id] {
				t.Fatal("unlisted multipart session", gotKey, id)
			}
			delete(pending, id)
			removed = append(removed, id)
			return nil
		},
		removeVersion: func(context.Context, string, string) error { t.Fatal("unexpected version"); return nil },
	}
	more, err := purgeTenantBatch(context.Background(), tenant, 1, io)
	if err != nil || !more || len(removed) != 1 || len(pending) != 1 {
		t.Fatal("first batch exceeded one exact upload", more, err, removed, pending)
	}
	more, err = purgeTenantBatch(context.Background(), tenant, 1, io)
	if err != nil || !more || len(removed) != 2 || len(pending) != 0 {
		t.Fatal("second batch did not remove remaining upload", more, err, removed, pending)
	}
	more, err = purgeTenantBatch(context.Background(), tenant, 1, io)
	if err != nil || more {
		t.Fatal("empty second pass did not converge", more, err)
	}
}

func TestTenantPurgeRefusesUnidentifiedMultipartSession(t *testing.T) {
	tenant := uuid.New()
	prefix, _ := tenantPrefix(tenant)
	deletes := 0
	io := tenantPurgeIO{
		uploads: func(context.Context, string) <-chan minio.ObjectMultipartInfo {
			rows := make(chan minio.ObjectMultipartInfo, 1)
			rows <- minio.ObjectMultipartInfo{Key: prefix + "object"}
			close(rows)
			return rows
		},
		versions: func(context.Context, string) <-chan minio.ObjectInfo {
			t.Fatal("advanced past unidentified upload")
			return nil
		},
		removeUpload:  func(context.Context, string, string) error { deletes++; return nil },
		removeVersion: func(context.Context, string, string) error { t.Fatal("unexpected version"); return nil },
	}
	if _, err := purgeTenantBatch(context.Background(), tenant, 1, io); !errors.Is(err, ErrInvalid) || deletes != 0 {
		t.Fatal("unidentified upload was accepted", err, deletes)
	}
}


func TestTenantPurgeVersionIdentityKeepsNullExplicit(t *testing.T) {
 if !validTenantPurgeVersionID("null") { t.Fatal("explicit legacy null version refused") }
 if ValidInterchangeVersionID("null") { t.Fatal("live immutable artifact version policy weakened") }
 for _,version:=range []string{"", "v\n", strings.Repeat("v",1025)} {
  if validTenantPurgeVersionID(version) { t.Fatal("invalid version accepted",len(version)) }
 }
}
