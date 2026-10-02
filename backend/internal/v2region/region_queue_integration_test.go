package v2region

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"testing"
	"time"
)

type unavailableRegionProfile struct{ calls int }

func (p *unavailableRegionProfile) CompileRegionProfile(context.Context, FrozenRegionPlan) (RegionExecutionPolicy, error) {
	p.calls++
	return RegionExecutionPolicy{}, ErrRegionInvalid
}
func TestIntegrationDisabledRegionQueueStopsBeforePreparation(t *testing.T) {
	raw := os.Getenv("PHOTO_REGION_QUEUE_DATABASE_URL")
	if raw == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	profile := &unavailableRegionProfile{}
	resolver := &regionInventoryResolver{}
	params := &regionParamsFixture{}
	closure := &regionTransportPublisher{}
	queue := QueueService{DB: pool, Profiles: profile, Resolver: resolver, Params: params, Closure: closure}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	// Preparation deliberately has no object service. Reaching it returns invalid,
	// proving unavailable is decided by the live SQL gate before any preparation.
	if _, err = queue.Queue(ctx, who, uuid.New(), uuid.New(), "region-disabled", RegionPreviewRequest{}); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("availability gate", err)
	}
	if profile.calls != 0 || resolver.tenant != uuid.Nil || params.document != uuid.Nil || closure.calls != 0 {
		t.Fatal("disabled queue invoked external provider")
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, err = queue.Queue(cancelled, who, uuid.New(), uuid.New(), "region-disabled", RegionPreviewRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled queue", err)
	}
}
