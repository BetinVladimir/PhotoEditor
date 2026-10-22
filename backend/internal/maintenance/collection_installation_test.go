package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type collectionInstallationDB struct {
	ready bool
	err error
	calls int
	bounded bool
	after context.CancelFunc
}

func (d *collectionInstallationDB) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	d.calls++
	deadline, ok := ctx.Deadline()
	d.bounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 5*time.Second
	if d.after != nil { d.after() }
	return collectionClaimRow{value: d.ready, err: d.err}
}

func TestCollectionCleanupInstallationRefusesUnavailableSchema(t *testing.T) {
	for _, ready := range []bool{false, true} {
		db := &collectionInstallationDB{ready: ready}
		err := RequireCollectionCleanup(context.Background(), db)
		if (err == nil) != ready || db.calls != 1 || !db.bounded { t.Fatalf("ready=%v calls=%d bounded=%v error=%v", ready, db.calls, db.bounded, err) }
	}
	if err := RequireCollectionCleanup(context.Background(), nil); err == nil { t.Fatal("missing database accepted") }
	failure := errors.New("database unavailable")
	if err := RequireCollectionCleanup(context.Background(), &collectionInstallationDB{err: failure}); !errors.Is(err, failure) { t.Fatal(err) }
}

func TestCollectionCleanupInstallationDoesNotPublishLateReadiness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db := &collectionInstallationDB{ready: true}
	if err := RequireCollectionCleanup(ctx, db); !errors.Is(err, context.Canceled) || db.calls != 0 { t.Fatal(err, db.calls) }
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	db = &collectionInstallationDB{ready: true, after: cancel}
	if err := RequireCollectionCleanup(ctx, db); !errors.Is(err, context.Canceled) { t.Fatal(err) }
}
