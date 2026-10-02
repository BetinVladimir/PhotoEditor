package v2input

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
	"time"
)

func TestIntegrationLensFactoryRoleIsolation(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := func(role string) *pgxpool.Pool {
		config, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			t.Fatal(err)
		}
		config.ConnConfig.RuntimeParams["role"] = role
		result, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(result.Close)
		return result
	}
	api, scheduler, gateway := pool("photo_api"), pool("photo_scheduler"), pool("photo_gateway")
	objects, err := objectstore.New(objectstore.Config{Endpoint: "http://127.0.0.1:9000", Bucket: "factory-test", AccessKey: "isolated-test", SecretKey: "isolated-test-secret", Development: true})
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	originalPin := LensQualificationPin{Ref{uuid.New(), digest}, digest, digest, digest}
	runtime := LensRuntime{API: api, Scheduler: scheduler, Objects: objects, Bucket: "factory-test", Registry: LensRegistryConfiguration{digest, []LensQualificationPin{originalPin}}}
	coordinator, err := runtime.Coordinator(ctx)
	if err != nil {
		t.Fatal("correct role construction", err)
	}
	selection, ok := coordinator.Selection.(SQLLensSelectionSource)
	if !ok || selection.DB != api {
		t.Fatal("selection did not use API role")
	}
	profiles, ok := coordinator.Profiles.(SQLLensProfileSource)
	if !ok || profiles.DB != api {
		t.Fatal("profile lookup did not use API role")
	}
	store, ok := coordinator.Sealer.(SQLLensDraftStore)
	if !ok || store.DB != scheduler {
		t.Fatal("sealer did not use scheduler role")
	}
	runtime.Registry.Pins[0].CoefficientsSHA = strings.Repeat("b", 64)
	if coordinator.QualificationPins[0] != originalPin || profiles.QualificationPins[0] != originalPin {
		t.Fatal("registry retained mutable caller pin slice")
	}
	for name, changed := range map[string]LensRuntime{
		"shared pool":          {API: api, Scheduler: api, Objects: objects, Bucket: runtime.Bucket, Registry: runtime.Registry},
		"wrong API role":       {API: gateway, Scheduler: scheduler, Objects: objects, Bucket: runtime.Bucket, Registry: runtime.Registry},
		"wrong scheduler role": {API: api, Scheduler: gateway, Objects: objects, Bucket: runtime.Bucket, Registry: runtime.Registry},
		"swapped roles":        {API: scheduler, Scheduler: api, Objects: objects, Bucket: runtime.Bucket, Registry: runtime.Registry},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := changed.Coordinator(ctx); err == nil {
				t.Fatal("role isolation bypass")
			}
		})
	}
}
