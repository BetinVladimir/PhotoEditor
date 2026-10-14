package maintenance

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

type tenantObservationStub struct {
	evidence objectstore.RestorePurgeStorageEvidence
	err      error
	stale    bool
	calls    int
}

func (s *tenantObservationStub) ObservePurgedTenant(ctx context.Context, tenant uuid.UUID, guard func(context.Context, uuid.UUID) error) (objectstore.RestorePurgeStorageEvidence, error) {
	s.calls++
	if err := guard(ctx, tenant); err != nil {
		return objectstore.RestorePurgeStorageEvidence{}, err
	}
	if !s.stale {
		s.evidence.ObservedAt = time.Now().UTC()
	}
	return s.evidence, s.err
}

func observedTenantBucket(tenant uuid.UUID, bucket string) objectstore.RestorePurgeStorageEvidence {
	id := tenant.String()
	return objectstore.RestorePurgeStorageEvidence{
		TenantID: tenant, Bucket: bucket,
		Prefixes: []string{"tenants/" + id + "/", "staging/tenants/" + id + "/", "originals/tenants/" + id + "/"},
		Passes:   2, ObservedAt: time.Now().UTC(),
	}
}

func TestObservePurgedTenantBucketsRequiresEveryExactBucket(t *testing.T) {
	tenant := uuid.New()
	first := &tenantObservationStub{evidence: observedTenantBucket(tenant, "primary")}
	second := &tenantObservationStub{evidence: observedTenantBucket(tenant, "archive")}
	var guardCalls atomic.Int32
	guard := func(_ context.Context, actual uuid.UUID) error {
		guardCalls.Add(1)
		if actual != tenant {
			return errors.New("foreign guard tenant")
		}
		return nil
	}
	targets := []TenantPurgeObservationTarget{{"primary", first}, {"archive", second}}
	proof, err := ObservePurgedTenantBuckets(context.Background(), tenant, targets, guard)
	if err != nil || len(proof) != 2 || proof[0].Bucket != "primary" || proof[1].Bucket != "archive" || first.calls != 1 || second.calls != 1 || guardCalls.Load() != 2 {
		t.Fatal("incomplete bucket proof", proof, err)
	}
	first.evidence.Prefixes[0] = "changed"
	if proof[0].Prefixes[0] == "changed" {
		t.Fatal("returned evidence shares mutable adapter slice")
	}
	second.evidence.Prefixes[1] = "foreign/"
	if partial, err := ObservePurgedTenantBuckets(context.Background(), tenant, targets, guard); err == nil || partial != nil {
		t.Fatal("partial or foreign bucket proof escaped", partial, err)
	}
	second.evidence.Prefixes[1] = "staging/tenants/" + tenant.String() + "/"
	second.stale = true
	second.evidence.ObservedAt = time.Now().Add(-time.Hour)
	if partial, err := ObservePurgedTenantBuckets(context.Background(), tenant, targets, guard); err == nil || partial != nil {
		t.Fatal("stale bucket proof escaped", partial, err)
	}
}

func TestObservePurgedTenantBucketsRejectsIncompleteInstallationBeforeIO(t *testing.T) {
	tenant := uuid.New()
	observer := &tenantObservationStub{evidence: observedTenantBucket(tenant, "primary")}
	guard := func(context.Context, uuid.UUID) error { return nil }
	for _, targets := range [][]TenantPurgeObservationTarget{nil, {{"primary", observer}, {"primary", observer}}, {{"missing", nil}}} {
		if proof, err := ObservePurgedTenantBuckets(context.Background(), tenant, targets, guard); err == nil || proof != nil || observer.calls != 0 {
			t.Fatal("incomplete installation reached IO", proof, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ObservePurgedTenantBuckets(ctx, tenant, []TenantPurgeObservationTarget{{"primary", observer}}, guard); !errors.Is(err, context.Canceled) || observer.calls != 0 {
		t.Fatal("cancellation reached storage", err)
	}
}
