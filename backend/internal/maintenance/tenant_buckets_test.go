package maintenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"sync/atomic"
	"testing"
	"time"
)

type tenantBucketStub struct {
	calls  int
	budget int
	more   bool
	err    error
}

func (s *tenantBucketStub) PurgeTenantBatch(_ context.Context, _ uuid.UUID, limit int) (bool, error) {
	s.calls++
	s.budget = limit
	return s.more, s.err
}

type tenantLateBucketStub struct{ calls int }

func (s *tenantLateBucketStub) PurgeTenantBatch(_ context.Context, _ uuid.UUID, _ int) (bool, error) {
	s.calls++
	return s.calls == 2, nil
}

type tenantReadyStub struct {
	calls int
	err   error
}

func (s *tenantReadyStub) Ready(context.Context) error { s.calls++; return s.err }

type tenantReadyPanics struct{}
func (tenantReadyPanics) Ready(context.Context) error { panic("private storage failure") }

type tenantReadyAtomicStub struct{ calls atomic.Int32 }

func (s *tenantReadyAtomicStub) Ready(context.Context) error { s.calls.Add(1); return nil }

type tenantReadyBarrier struct {
	entered chan<- struct{}
	release <-chan struct{}
}

type tenantReadyIgnoresCancellation struct {
	entered chan<- struct{}
	release <-chan struct{}
}

func (s tenantReadyIgnoresCancellation) Ready(context.Context) error {
	s.entered <- struct{}{}
	<-s.release
	return nil
}

func (s tenantReadyBarrier) Ready(ctx context.Context) error {
	select {
	case s.entered <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type tenantReadyBounded struct {
	active, max *atomic.Int32
	entered     chan<- struct{}
	release     <-chan struct{}
}

func (s tenantReadyBounded) Ready(ctx context.Context) error {
	count := s.active.Add(1)
	defer s.active.Add(-1)
	for previous := s.max.Load(); count > previous; previous = s.max.Load() {
		if s.max.CompareAndSwap(previous, count) {
			break
		}
	}
	select {
	case s.entered <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestTenantPurgeReadinessProbesIndependentBucketsConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- readyTenantBuckets(ctx, []tenantBucketReadiness{
			tenantReadyBarrier{entered, release}, tenantReadyBarrier{entered, release},
		})
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("readiness serialized independent buckets")
		}
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal("ready buckets rejected", err)
	}
}

func TestTenantPurgeReadinessDeadlineSurvivesUnresponsiveTarget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	result := make(chan error, 1)
	go func() {
		result <- readyTenantBuckets(ctx, []tenantBucketReadiness{tenantReadyIgnoresCancellation{entered, release}})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("readiness probe did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("unresponsive target did not fail by deadline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("readiness blocked past deadline")
	}
}

func TestTenantPurgeReadinessPanicReleasesAllGlobalSlots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stores := make([]tenantBucketReadiness, cap(tenantReadinessSlots))
	for i := range stores { stores[i] = tenantReadyPanics{} }
	if err := readyTenantBuckets(ctx, stores); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("panicking readiness did not fail promptly", err)
	}
	ready := &tenantReadyStub{}
	if err := readyTenantBuckets(ctx, []tenantBucketReadiness{ready}); err != nil || ready.calls != 1 {
		t.Fatal("panic leaked readiness slots", err, ready.calls)
	}
}

func TestTenantPurgeReadinessBoundsConcurrentStorageProbes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered := make(chan struct{}, 40)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	stores := make([]tenantBucketReadiness, 40)
	for index := range stores {
		stores[index] = tenantReadyBounded{&active, &maximum, entered, release}
	}
	result := make(chan error, 1)
	go func() { result <- readyTenantBuckets(ctx, stores) }()
	for index := 0; index < 32; index++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("readiness did not fill bounded probe slots")
		}
	}
	close(release)
	if err := <-result; err != nil || maximum.Load() > 32 || maximum.Load() < 2 {
		t.Fatal("readiness concurrency bound failed", err, maximum.Load())
	}
}

func TestTenantPurgeReadinessChecksMaximumConfiguredTargets(t *testing.T) {
	stores := make([]tenantBucketReadiness, 256)
	all := make([]*tenantReadyStub, len(stores))
	for index := range stores {
		all[index] = &tenantReadyStub{}
		stores[index] = all[index]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := readyTenantBuckets(ctx, stores); err != nil {
		t.Fatal("maximum target inventory rejected", err)
	}
	for index, store := range all {
		if store.calls != 1 {
			t.Fatal("configured target was not checked exactly once", index, store.calls)
		}
	}
}

func TestTenantPurgeReadinessCancellationSkipsQueuedTargets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{}, 32)
	release := make(chan struct{})
	stores := make([]tenantBucketReadiness, 256)
	for index := 0; index < 32; index++ {
		stores[index] = tenantReadyIgnoresCancellation{entered, release}
	}
	queued := make([]*tenantReadyAtomicStub, len(stores)-32)
	for index := range queued {
		queued[index] = &tenantReadyAtomicStub{}
		stores[index+32] = queued[index]
	}
	result := make(chan error, 1)
	go func() { result <- readyTenantBuckets(ctx, stores) }()
	for index := 0; index < 32; index++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			cancel()
			close(release)
			t.Fatal("readiness workers did not fill the global probe limit")
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled readiness accepted", err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("cancelled readiness did not return promptly")
	}
	for index, store := range queued {
		if store.calls.Load() != 0 {
			close(release)
			t.Fatal("queued target probed after cancellation", index)
		}
	}
	close(release)
	deadline := time.After(time.Second)
	for len(tenantReadinessSlots) != 0 {
		select {
		case <-deadline:
			t.Fatal("cancelled readiness left occupied probe slots")
		case <-time.After(time.Millisecond):
		}
	}
	for index, store := range queued {
		if store.calls.Load() != 0 {
			t.Fatal("queued target probed after blocked probes returned", index)
		}
	}
}

func TestTenantPurgeReadinessBoundsConcurrentRequestsTogether(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered := make(chan struct{}, 80)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	stores := make([]tenantBucketReadiness, 40)
	for index := range stores {
		stores[index] = tenantReadyBounded{&active, &maximum, entered, release}
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- readyTenantBuckets(ctx, stores) }()
	}
	for index := 0; index < 32; index++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("shared readiness slots were not filled")
		}
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal("concurrent readiness failed", err)
		}
	}
	if maximum.Load() > 32 {
		t.Fatal("concurrent health requests exceeded shared storage limit", maximum.Load())
	}
}

func TestTenantPurgeStartupAndHealthRequireEveryBucketReady(t *testing.T) {
	first, second := &tenantReadyStub{}, &tenantReadyStub{}
	stores := []tenantBucketReadiness{first, second}
	if err := readyTenantBuckets(context.Background(), stores); err != nil || first.calls != 1 || second.calls != 1 {
		t.Fatal("all-bucket startup readiness missing", err)
	}
	second.err = errors.New("secondary bucket unavailable")
	if err := readyTenantBuckets(context.Background(), stores); !errors.Is(err, second.err) || first.calls != 2 || second.calls != 2 {
		t.Fatal("secondary health failure hidden", err)
	}
	if err := readyTenantBuckets(context.Background(), []tenantBucketReadiness{first, nil}); err == nil || first.calls != 2 {
		t.Fatal("missing bucket reached readiness IO", err)
	}
	if err := readyTenantBuckets(context.Background(), nil); err == nil {
		t.Fatal("empty bucket inventory accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := readyTenantBuckets(ctx, stores); !errors.Is(err, context.Canceled) || first.calls != 2 {
		t.Fatal("cancelled readiness performed IO", err)
	}
}

func TestTenantPurgeBucketsSharesBudgetAndRequiresEveryEmptyPass(t *testing.T) {
	first, second, third := &tenantBucketStub{}, &tenantBucketStub{more: true}, &tenantBucketStub{}
	buckets := TenantPurgeBuckets{{"primary", first}, {"archive", second}, {"legacy", third}}
	more, err := buckets.PurgeTenantBatch(context.Background(), uuid.New(), 8)
	if err != nil || !more || first.budget != 3 || second.budget != 3 || third.budget != 2 || first.calls != 1 || second.calls != 1 || third.calls != 1 {
		t.Fatal("tenant bucket budget or completion changed", more, err, first, second, third)
	}
	second.more = false
	more, err = buckets.PurgeTenantBatch(context.Background(), uuid.New(), 3)
	if err != nil || more || first.calls != 2 || second.calls != 2 || third.calls != 2 {
		t.Fatal("all-bucket empty proof missing", more, err)
	}
}

func TestTenantPurgeBucketsRefusesIncompleteInstallation(t *testing.T) {
	for _, buckets := range []TenantPurgeBuckets{nil, {{"", &tenantBucketStub{}}}, {{"same", &tenantBucketStub{}}, {"same", &tenantBucketStub{}}}, {{"missing", nil}}} {
		if more, err := buckets.PurgeTenantBatch(context.Background(), uuid.New(), 2); err == nil || more {
			t.Fatal("accepted incomplete bucket installation", buckets)
		}
	}
	first, second := &tenantBucketStub{}, &tenantBucketStub{}
	buckets := TenantPurgeBuckets{{"a", first}, {"b", second}}
	if _, err := buckets.PurgeTenantBatch(context.Background(), uuid.New(), 1); err == nil || first.calls != 0 || second.calls != 0 {
		t.Fatal("insufficient global budget reached storage")
	}
}

func TestTenantPurgeBucketsRefusesCancellationAndPartialFailure(t *testing.T) {
	first, second := &tenantBucketStub{}, &tenantBucketStub{err: errors.New("storage unavailable")}
	buckets := TenantPurgeBuckets{{"a", first}, {"b", second}}
	if more, err := buckets.PurgeTenantBatch(context.Background(), uuid.New(), 2); err == nil || more || first.calls != 1 || second.calls != 1 {
		t.Fatal("partial bucket failure reported empty", more, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := buckets.PurgeTenantBatch(ctx, uuid.New(), 2); !errors.Is(err, context.Canceled) || first.calls != 1 {
		t.Fatal("cancelled bucket scan began IO", err)
	}
}

func TestTenantPurgeBucketInstallationNames(t *testing.T) {
	names, err := tenantPurgeBucketNames("primary", "archive, legacy")
	if err != nil || len(names) != 3 || names[0] != "primary" || names[1] != "archive" || names[2] != "legacy" {
		t.Fatal("bucket installation lost order", names, err)
	}
	for _, extra := range []string{"primary", "archive,archive", ",archive", "archive,", "archive,,legacy"} {
		if names, err := tenantPurgeBucketNames("primary", extra); err == nil {
			t.Fatal("accepted incomplete bucket inventory", extra, names)
		}
	}
}

func TestTenantCleanerCannotFinishWhileSecondaryBucketHasRemnants(t *testing.T) {
	repo := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
	primary, secondary := &tenantBucketStub{}, &tenantBucketStub{more: true}
	cleaner := TenantCleaner{Repository: repo, Objects: TenantPurgeBuckets{{"primary", primary}, {"secondary", secondary}}, Batch: 8}
	changed, err := cleaner.CleanOne(context.Background())
	if err != nil || !changed || repo.done || !repo.checkpointed {
		t.Fatal("secondary bucket allowed metadata purge", changed, err, repo)
	}
	secondary.more = false
	repo.checkpointed = false
	changed, err = cleaner.CleanOne(context.Background())
	if err != nil || !changed || !repo.done {
		t.Fatal("all-bucket empty result did not finish", changed, err, repo)
	}
}

func TestTenantCleanerRechecksSecondaryBucketAfterSettlement(t *testing.T) {
	repo := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
	primary := &tenantBucketStub{}
	secondary := &tenantLateBucketStub{}
	cleaner := TenantCleaner{Repository: repo, Objects: TenantPurgeBuckets{{"primary", primary}, {"secondary", secondary}}, Batch: 8}
	changed, err := cleaner.CleanOne(context.Background())
	if err != nil || !changed || !repo.settled || !repo.checkpointed || repo.done || primary.calls != 2 || secondary.calls != 2 {
		t.Fatal("late secondary-bucket remnant allowed irreversible finish", changed, err, repo, primary, secondary)
	}
}
