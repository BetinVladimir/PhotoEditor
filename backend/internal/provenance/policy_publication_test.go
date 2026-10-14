package provenance

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type signingPolicyStoreStub struct {
	signingLineageStoreStub
	run                         uuid.UUID
	afterReserve, afterComplete func()
}

func (s *signingPolicyStoreStub) ReserveSigningPolicy(ctx context.Context, who auth.Identity, ready ReadySigningExport, expected v2input.Ref, raw []byte) (SigningPolicyReservation, error) {
	reservation, err := s.ReserveSigningLineage(ctx, who, ready, expected, raw)
	if s.afterReserve != nil {
		s.afterReserve()
	}
	version := ""
	if reservation.Ready != nil {
		version = "v1"
	}
	return SigningPolicyReservation{Reservation: reservation, StorageRun: s.run, ReadyVersion: version}, err
}
func (s *signingPolicyStoreStub) CompleteSigningPolicy(ctx context.Context, who auth.Identity, ready ReadySigningExport, expected v2input.Ref, raw []byte, reservation SigningPolicyReservation, object objectstore.Object) (v2input.Ref, error) {
	key, err := objectstore.DocumentKey(ready.Tenant, s.run, expected.ArtifactID)
	if err != nil || object.Key != key {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	receipt, err := s.CompleteSigningLineage(ctx, who, ready, expected, raw, reservation.Reservation, object)
	if s.afterComplete != nil {
		s.afterComplete()
	}
	return receipt, err
}

var _ SigningPolicyStore = (*signingPolicyStoreStub)(nil)

func TestSigningPolicyPublicationImmutableProtocol(t *testing.T) {
	for _, mode := range []string{"valid", "ready", "ready_new_latest", "ready_bad_recover", "ready_unavailable", "ready_wrong_version", "exists", "recover", "mutate", "bad_recover", "object", "reservation", "token", "wrong_ready", "receipt", "revoked", "cancelled", "foreign", "missing_storage_run", "write_only"} {
		t.Run(mode, func(t *testing.T) {
			config, params, now := signingRegistryFixture(t)
			registry, err := NewSigningPolicyRegistry(config, &signingAuthorityStub{})
			if err != nil {
				t.Fatal(err)
			}
			tenant := uuid.New()
			registry, err = registry.ForTenant(tenant)
			if err != nil {
				t.Fatal(err)
			}
			params.Policy = registry.policies[params.PolicyID].ref
			ready := ReadySigningExport{Tenant: tenant, Project: uuid.New(), ExportID: uuid.New(), Run: uuid.New(), Task: uuid.New(), Ref: params.Export, Object: objectstore.Object{ContentType: "image/jpeg"}}
			catalog := &signingCatalogStub{ready: ready, change: mode == "revoked"}
			storeMode, objectMode := mode, mode
			if mode == "ready_bad_recover" {
				storeMode, objectMode = "ready", "bad_recover"
			}
			if mode == "ready_unavailable" {
				storeMode, objectMode = "ready", "unavailable"
			}
			if mode == "ready_wrong_version" {
				storeMode, objectMode = "ready", "wrong_version"
			}
			if mode == "ready_new_latest" {
				storeMode, objectMode = "ready", "latest_changed"
			}
			store := &signingPolicyStoreStub{signingLineageStoreStub: signingLineageStoreStub{mode: storeMode}, run: uuid.New()}
			objects := &signingLineageObjectsStub{mode: objectMode}
			var publicationObjects artifactjson.Objects = objects
			if mode == "write_only" {
				publicationObjects = signingWriteOnly{Objects: objects}
			}
			_, objects.body, err = registry.PolicyDocument(context.Background(), tenant, params, ready.Object.ContentType, now)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if mode == "foreign" {
				ready.Tenant = uuid.New()
			}
			if mode == "missing_storage_run" {
				store.run = uuid.Nil
			}
			result, err := (ImmutableSigningPolicyPublisher{Catalog: catalog, Registry: registry, Store: store, Objects: publicationObjects, Clock: func() time.Time { return now }}).PublishSigningPolicy(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ready, params)
			success := mode == "valid" || mode == "ready" || mode == "ready_new_latest" || mode == "exists" || mode == "recover" || mode == "mutate"
			if success {
				if err != nil || result != params.Policy {
					t.Fatal("publication refused", err)
				}
				if mode == "ready" || mode == "ready_new_latest" {
					if objects.puts != 0 || objects.recovers != 0 || objects.opens != 1 || store.completes != 0 {
						t.Fatal("ready replay wrote storage")
					}
				} else if objects.puts != 1 || store.completes != 1 {
					t.Fatal("incomplete publication")
				}
			} else if err == nil || result.ArtifactID != uuid.Nil {
				t.Fatal("invalid publication accepted")
			}
			if (mode == "foreign" || mode == "cancelled" || mode == "write_only") && store.reserves != 0 {
				t.Fatal("invalid scope reserved storage")
			}
			if (mode == "object" || mode == "bad_recover" || mode == "revoked") && store.completes != 0 {
				t.Fatal("invalid object or authority completed")
			}
		})
	}
}

type signingPolicyHookObjects struct {
	signingLineageObjectsStub
	afterPut, afterRecover func()
}

func (s *signingPolicyHookObjects) PutImmutable(ctx context.Context, key string, reader io.Reader, size int64, sha, media string) (objectstore.Object, error) {
	object, err := s.signingLineageObjectsStub.PutImmutable(ctx, key, reader, size, sha, media)
	if s.afterPut != nil {
		s.afterPut()
	}
	return object, err
}
func (s *signingPolicyHookObjects) RecoverImmutable(ctx context.Context, key string, size int64, sha, media string) (objectstore.Object, error) {
	object, err := s.signingLineageObjectsStub.RecoverImmutable(ctx, key, size, sha, media)
	if s.afterRecover != nil {
		s.afterRecover()
	}
	return object, err
}

func TestSigningPolicyPublicationRepeatsAuthorityAcrossIO(t *testing.T) {
	for _, phase := range []string{"reserve", "put", "recover", "complete"} {
		for _, failure := range []string{"expired", "operator", "cancelled"} {
			t.Run(phase+"/"+failure, func(t *testing.T) {
				config, params, now := signingRegistryFixture(t)
				authority := &signingAuthorityStub{}
				registry, err := NewSigningPolicyRegistry(config, authority)
				if err != nil {
					t.Fatal(err)
				}
				tenant := uuid.New()
				registry, err = registry.ForTenant(tenant)
				if err != nil {
					t.Fatal(err)
				}
				params.Policy = registry.policies[params.PolicyID].ref
				ready := ReadySigningExport{Tenant: tenant, Project: uuid.New(), ExportID: uuid.New(), Run: uuid.New(), Task: uuid.New(), Ref: params.Export, Object: objectstore.Object{ContentType: "image/jpeg"}}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				clock := now
				denied := errors.New("operator revoked during publication")
				hook := func() {
					switch failure {
					case "expired":
						clock = config.Policies[0].NotAfter
					case "operator":
						authority.err = denied
					case "cancelled":
						cancel()
					}
				}
				store := &signingPolicyStoreStub{run: uuid.New()}
				objects := &signingPolicyHookObjects{}
				switch phase {
				case "reserve":
					store.afterReserve = hook
				case "put":
					objects.afterPut = hook
				case "recover":
					objects.mode = "recover"
					objects.afterRecover = hook
				case "complete":
					store.afterComplete = hook
				}
				result, err := (ImmutableSigningPolicyPublisher{Catalog: &signingCatalogStub{ready: ready}, Registry: registry, Store: store, Objects: objects, Clock: func() time.Time { return clock }}).PublishSigningPolicy(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, ready, params)
				if err == nil || result.ArtifactID != uuid.Nil {
					t.Fatal("revoked publication returned authority")
				}
				if failure == "operator" && !errors.Is(err, denied) {
					t.Fatal("operator cause lost", err)
				}
				if failure == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
				if phase == "reserve" && objects.puts != 0 {
					t.Fatal("revoked reservation wrote object")
				}
				if phase != "complete" && store.completes != 0 {
					t.Fatal("revoked authority completed publication")
				}
				if phase == "complete" && store.completes != 1 {
					t.Fatal("completion boundary not reached")
				}
			})
		}
	}
}
