package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/delivery"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDeliveryRuntimeEnvironmentRequiresExplicitEnablementAndBoundedResources(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		env     map[string]string
		bad     bool
		enabled bool
	}{
		{name: "default"},
		{name: "disabled", env: map[string]string{"PHOTO_DELIVERY_SCHEDULER_ENABLED": "false"}},
		{name: "enabled", env: map[string]string{"PHOTO_DELIVERY_SCHEDULER_ENABLED": "true"}, enabled: true},
		{name: "invalid_flag", env: map[string]string{"PHOTO_DELIVERY_SCHEDULER_ENABLED": "1"}, bad: true},
		{name: "negative_ram", env: map[string]string{"PHOTO_DELIVERY_SCHEDULER_ENABLED": "true", "PHOTO_DELIVERY_RAM_BYTES": "-1"}, bad: true},
		{name: "zero_output", env: map[string]string{"PHOTO_DELIVERY_SCHEDULER_ENABLED": "true", "PHOTO_DELIVERY_OUTPUT_BYTES": "0"}, bad: true},
		{name: "overflow_scratch", env: map[string]string{"PHOTO_DELIVERY_SCHEDULER_ENABLED": "true", "PHOTO_DELIVERY_SCRATCH_BYTES": "9007199254740992"}, bad: true},
		{name: "unbounded_page", env: map[string]string{"PHOTO_DELIVERY_SCHEDULER_ENABLED": "true", "PHOTO_DELIVERY_PAGE_SIZE": "101"}, bad: true},
		{name: "unbounded_work", env: map[string]string{"PHOTO_DELIVERY_SCHEDULER_ENABLED": "true", "PHOTO_DELIVERY_WORK_SECONDS": "7201"}, bad: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			c, err := DeliveryRuntimeFromEnvironment(func(key string) string { return scenario.env[key] })
			if (err != nil) != scenario.bad {
				t.Fatal(c, err)
			}
			if !scenario.bad && (c.Enabled != scenario.enabled || c.PageSize != 10 || c.Limits.RAMBytes != 8<<30) {
				t.Fatal(c)
			}
		})
	}
	c, err := DeliveryRuntimeFromEnvironment(func(key string) string {
		return map[string]string{"PHOTO_DELIVERY_SCHEDULER_ENABLED": "true", "PHOTO_DELIVERY_RAM_BYTES": "123456", "PHOTO_DELIVERY_PAGE_SIZE": "1", "PHOTO_DELIVERY_WORK_SECONDS": "600"}[key]
	})
	if err != nil || c.Limits.RAMBytes != 123456 || c.PageSize != 1 || c.WorkTimeout != 10*time.Minute {
		t.Fatal(c, err)
	}
	if _, err := DeliveryRuntimeFromEnvironment(nil); err == nil {
		t.Fatal("nil environment accepted")
	}
}

// Construction must install the full assembly coordinator without object/SQL IO.
func TestDeliveryRuntimeInstallsAssemblyWithSharedLimits(t *testing.T) {
	db := &pgxpool.Pool{}
	objects := deliveryAssemblyNoIO{}
	reader := &deliveryJSONReader{}
	sha := strings.Repeat("a", 64)
	profiles := map[string]string{"srgb_v1": sha, "work_rec2020_gamma24_v1": sha}
	config := DeliveryRuntimeConfig{Enabled: true, PageSize: 3, Limits: DeliveryDerivativeLimits{RAMBytes: 8 << 30, ScratchBytes: 8 << 30, OutputBytes: 2 << 30}}
	instance := uuid.New()
	job, err := NewDeliveryExpansionJob(db, objects, reader, instance, "photos", sha, profiles, Registry{}, config)
	if err != nil {
		t.Fatal(err)
	}
	router, routed := job.Driver.Children.(DeliveryChildRouter)
	if !routed {
		t.Fatal("mixed child router missing")
	}
	wm, ok := router.Watermark.(DeliveryWatermarkChildPipeline)
	if !ok || wm.DB != db || wm.Reader != reader || wm.Bucket != "photos" || wm.Limits != config.Limits {
		t.Fatal("watermark pipeline missing")
	}
	resolver, ok := wm.Resolver.(SQLDeliveryWatermarkChildResolver)
	if !ok || resolver.DB != db || resolver.WorkingICCSHA != sha || resolver.TargetICCs["srgb_v1"] != sha {
		t.Fatal("watermark resolver missing")
	}
	if _, ok := job.Driver.Observer.(DeliveryChildRouter); !ok {
		t.Fatal("mixed observer missing")
	}
	if store, ok := job.Driver.Store.(DeliveryRuntimePlanStore); !ok || store.Transactions != db || store.Instance != instance || store.Bucket != "photos" {
		t.Fatal("current-authority loader missing")
	}
	assembly, ok := job.Driver.Assembly.(DeliveryAssemblyPipeline)
	if job.Cleanup == nil {
		t.Fatal("quota cleanup not installed")
	}
	if !ok || assembly.DB != db || assembly.Bucket != "photos" || assembly.Limits != config.Limits || job.Driver.PageSize != 3 {
		t.Fatal("assembly pipeline not installed", job)
	}
	for _, bad := range []string{strings.Repeat("z", 64), strings.Repeat("A", 64), ""} {
		if _, err := NewDeliveryExpansionJob(db, objects, reader, instance, "photos", bad, profiles, Registry{}, config); err == nil {
			t.Fatal("invalid ICC digest accepted")
		}
	}
	if _, err := NewDeliveryExpansionJob(db, objects, reader, uuid.Nil, "photos", sha, profiles, Registry{}, config); err == nil {
		t.Fatal("unbound delivery scheduler accepted")
	}
	config.WorkTimeout = 2*time.Hour + time.Second
	if _, err := NewDeliveryExpansionJob(db, objects, reader, instance, "photos", sha, profiles, Registry{}, config); err == nil {
		t.Fatal("unbounded delivery work accepted")
	}
}

func TestDeliveryJobCountsActivationWithoutNewChildAdmissions(t *testing.T) {
	plan, parent, _, _, _ := deliveryExportPlanFixture(t, nil)
	for _, state := range []string{"activated", "finalized_failed", "pending", "failed"} {
		t.Run(state, func(t *testing.T) {
			calls := []string{}
			store := &deliveryDriverStore{calls: &calls, plan: plan, lease: &delivery.ExpansionLease{Run: parent, ParentTask: plan.TaskID(), SlotCount: 1, Cursor: 1}}
			job := DeliveryExpansionJob{Driver: DeliveryExpansionDriver{Store: store, Children: deliveryDriverChild{calls: &calls}, Assembly: &deliveryDriverAssembly{state: state}, PageSize: 1}}
			changed, err := job.ProcessOne(context.Background())
			if state == "failed" {
				if err == nil || changed {
					t.Fatal("unknown assembly outcome admitted", changed, err)
				}
				return
			}
			if err != nil || changed != (state == "activated" || state == "finalized_failed") {
				t.Fatal("wrong scheduler work credit", changed, err)
			}
		})
	}
}

type deliveryRuntimeCleanup struct {
	calls    *[]string
	released bool
	err      error
	bounded  bool
}

func (c *deliveryRuntimeCleanup) ReleaseNextQuota(ctx context.Context) (bool, error) {
	*c.calls = append(*c.calls, "cleanup")
	deadline, ok := ctx.Deadline()
	c.bounded = ok && time.Until(deadline) <= 5*time.Minute
	return c.released, c.err
}

func TestDeliveryJobCleanupWorkAndErrors(t *testing.T) {
	for _, scenario := range []string{"released", "empty", "cleanup_error", "claim_error", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			calls := []string{}
			cleanup := &deliveryRuntimeCleanup{calls: &calls, released: scenario == "released" || scenario == "claim_error"}
			store := &deliveryDriverStore{calls: &calls}
			if scenario == "cleanup_error" {
				cleanup.err = errors.New("cleanup unavailable")
			}
			if scenario == "claim_error" {
				store.fail = "claim"
			}
			job := DeliveryExpansionJob{Cleanup: cleanup, Driver: DeliveryExpansionDriver{Store: store, Children: deliveryDriverChild{calls: &calls}, PageSize: 1}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancel" {
				cancel()
			}
			changed, err := job.ProcessOne(ctx)
			expected := []string{"cleanup", "claim"}
			if scenario == "cancel" {
				expected = []string{}
			}
			if !reflect.DeepEqual(calls, expected) {
				t.Fatal("unexpected scheduler order", calls)
			}
			if changed != (scenario == "released" || scenario == "claim_error") {
				t.Fatal("lost cleanup progress", changed)
			}
			if (err != nil) != (scenario == "cleanup_error" || scenario == "claim_error" || scenario == "cancel") {
				t.Fatal(err)
			}
			if scenario != "cancel" && !cleanup.bounded {
				t.Fatal("cleanup lacks job deadline")
			}
		})
	}
}

func TestDeliveryCleanupFailureDoesNotStarveAcceptedAssembly(t *testing.T) {
	plan, parent, _, _, _ := deliveryExportPlanFixture(t, nil)
	calls := []string{}
	cleanupFailure := errors.New("cleanup unavailable")
	job := DeliveryExpansionJob{
		Cleanup: &deliveryRuntimeCleanup{calls: &calls, err: cleanupFailure},
		Driver: DeliveryExpansionDriver{
			Store: &deliveryDriverStore{calls: &calls, plan: plan,
				lease: &delivery.ExpansionLease{Run: parent, ParentTask: plan.TaskID(), SlotCount: 1, Cursor: 1}},
			Children: deliveryDriverChild{calls: &calls},
			Assembly: &deliveryDriverAssembly{state: "activated"}, PageSize: 1,
		},
	}
	changed, err := job.ProcessOne(context.Background())
	if !changed || !errors.Is(err, cleanupFailure) || len(calls) < 2 || calls[0] != "cleanup" || calls[1] != "claim" {
		t.Fatal("cleanup error starved accepted assembly", changed, err, calls)
	}
}

type deliveryRuntimeMetadata struct {
	calls    *[]string
	progress bool
	err      error
}

func (m deliveryRuntimeMetadata) ProcessOne(context.Context) (bool, error) {
	*m.calls = append(*m.calls, "metadata")
	return m.progress, m.err
}

func TestDeliveryMetadataProgressDoesNotStarveOrdinaryWork(t *testing.T) {
	for _, scenario := range []string{"progress", "empty", "error"} {
		t.Run(scenario, func(t *testing.T) {
			calls := []string{}
			metadata := deliveryRuntimeMetadata{calls: &calls, progress: scenario == "progress"}
			if scenario == "error" {
				metadata.err = errors.New("metadata failed")
			}
			job := DeliveryExpansionJob{Metadata: metadata,
				Driver: DeliveryExpansionDriver{Store: &deliveryDriverStore{calls: &calls}, Children: deliveryDriverChild{calls: &calls}, PageSize: 1}}
			changed, err := job.ProcessOne(context.Background())
			if scenario == "error" {
				if err == nil || !reflect.DeepEqual(calls, []string{"metadata", "claim"}) {
					t.Fatal("metadata error starved ordinary claim", changed, err, calls)
				}
				return
			}
			if err != nil || changed != (scenario == "progress") || !reflect.DeepEqual(calls, []string{"metadata", "claim"}) {
				t.Fatal("ordinary delivery starved by metadata", changed, err, calls)
			}
		})
	}
}

func TestDeliveryMetadataAndOrdinaryFailuresAreBothReported(t *testing.T) {
	calls := []string{}
	metadataFailure := errors.New("metadata failed")
	store := &deliveryDriverStore{calls: &calls, fail: "claim"}
	job := DeliveryExpansionJob{Metadata: deliveryRuntimeMetadata{calls: &calls, err: metadataFailure}, Driver: DeliveryExpansionDriver{Store: store, Children:deliveryDriverChild{calls:&calls}, PageSize: 1}}
	_, err := job.ProcessOne(context.Background())
	if !errors.Is(err, metadataFailure) || len(calls) != 2 || calls[0] != "metadata" || calls[1] != "claim" {
		t.Fatal("independent failure masked", calls, err)
	}
}
