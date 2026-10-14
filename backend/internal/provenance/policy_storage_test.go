package provenance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestSigningPolicyStoragePinsRejectForeignIdentityAndChangedContent(t *testing.T) {
	for _, mode := range []string{"valid", "foreign_tenant", "foreign_artifact", "hash", "oversized", "version", "source_sha", "source_mime", "source_bytes", "session", "bucket", "source_task"} {
		t.Run(mode, func(t *testing.T) {
			config, params, _ := signingRegistryFixture(t)
			registry, err := NewSigningPolicyRegistry(config, &signingAuthorityStub{})
			if err != nil {
				t.Fatal(err)
			}
			tid := uuid.New()
			registry, err = registry.ForTenant(tid)
			if err != nil {
				t.Fatal(err)
			}
			installed := registry.policies[params.PolicyID]
			expected, raw := installed.ref, append([]byte(nil), installed.body...)
			ready := ReadySigningExport{Tenant: tid, Project: uuid.New(), ExportID: uuid.New(), Version: uuid.New(), Run: uuid.New(), Task: uuid.New(), Ref: params.Export, Object: objectstore.Object{Key: "source", VersionID: "v1", SHA256: params.Export.SHA256, Bytes: 100, ContentType: "image/jpeg"}}
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			store := SQLSigningPolicyStore{Bucket: "photos"}
			switch mode {
			case "foreign_tenant":
				ready.Tenant = uuid.New()
			case "foreign_artifact":
				expected.ArtifactID = uuid.New()
			case "hash":
				raw[0] = 'x'
			case "oversized":
				raw = []byte(strings.Repeat("x", (1<<20)+1))
			case "version":
				ready.Object.VersionID = "null"
			case "source_sha":
				ready.Object.SHA256 = strings.Repeat("f", 64)
			case "source_mime":
				ready.Object.ContentType = "image/tiff"
			case "source_bytes":
				ready.Object.Bytes = 0
			case "session":
				who.SessionID = uuid.Nil
			case "bucket":
				store.Bucket = "x"
			case "source_task":
				ready.Task = uuid.Nil
			}
			policy, pins, err := store.pins(who, ready, expected, raw)
			if mode != "valid" {
				if err == nil {
					t.Fatal("invalid storage authority accepted")
				}
				return
			}
			if err != nil || policy.ID != params.PolicyID {
				t.Fatal("valid policy rejected", err)
			}
			var values map[string]json.RawMessage
			if err := json.Unmarshal(pins, &values); err != nil || len(values) != 10 {
				t.Fatal("source pin wire changed", err)
			}
			for _, key := range []string{"project", "version", "task", "run", "artifact", "sha256", "key", "object_version", "bytes", "media_type"} {
				if values[key] == nil {
					t.Fatal("missing source pin", key)
				}
			}
		})
	}
}

func TestSigningPolicySQLStoreRejectsCancellationBeforeDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := SQLSigningPolicyStore{}
	if _, err := store.ReserveSigningPolicy(ctx, auth.Identity{}, ReadySigningExport{}, v2input.Ref{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored", err)
	}
	if _, err := store.CompleteSigningPolicy(ctx, auth.Identity{}, ReadySigningExport{}, v2input.Ref{}, nil, SigningPolicyReservation{}, objectstore.Object{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored", err)
	}
}
