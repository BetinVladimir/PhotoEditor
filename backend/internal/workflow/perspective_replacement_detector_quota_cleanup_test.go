package workflow

import (
	"context"
	"errors"
	"testing"
)

func TestReplacementDetectorQuotaCleanupBoundedAndPropagatesOutcome(t *testing.T) {
	failure := errors.New("database unavailable")
	for _, tc := range []struct {
		changed bool
		err     error
	}{{true, nil}, {false, nil}, {false, failure}} {
		db := &linesLifecycleDB{rows: []linesLifecycleRow{linesLifecycleRow{value: tc.changed, err: tc.err}}}
		changed, err := (ReplacementDetectorQuotaCleanup{DB: db}).ProcessOne(context.Background())
		if changed != tc.changed || !errors.Is(err, tc.err) || !db.bounded || len(db.queries) != 1 || db.queries[0] != "SELECT photo.release_next_v29_replacement_detector_quota()" {
			t.Fatal("quota step result/bounds", changed, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (ReplacementDetectorQuotaCleanup{}).ProcessOne(ctx); err != context.Canceled {
		t.Fatal("cancellation", err)
	}
	if _, err := (ReplacementDetectorQuotaCleanup{}).ProcessOne(context.Background()); err == nil {
		t.Fatal("missing database accepted")
	}
}
