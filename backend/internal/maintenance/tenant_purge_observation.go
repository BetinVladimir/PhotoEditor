package maintenance

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

// Each target is an independently configured physical bucket. The caller is
// responsible for supplying the complete writer-destination inventory.
type TenantPurgeObservationTarget struct {
	Name     string
	Observer interface {
		ObservePurgedTenant(context.Context, uuid.UUID, func(context.Context, uuid.UUID) error) (objectstore.RestorePurgeStorageEvidence, error)
	}
}

type tenantPurgeObservationResult struct {
	index    int
	evidence objectstore.RestorePurgeStorageEvidence
	err      error
}

// A slow adapter may outlive a canceled caller; this process-wide bound keeps
// concurrent restore observations from spawning unlimited storage scans.
var tenantObservationSlots = make(chan struct{}, 32)

func observeTenantPurgeTarget(ctx context.Context, target TenantPurgeObservationTarget, tenant uuid.UUID, guard func(context.Context, uuid.UUID) error) (evidence objectstore.RestorePurgeStorageEvidence, err error) {
	defer func() {
		<-tenantObservationSlots
		if recover() != nil {
			evidence = objectstore.RestorePurgeStorageEvidence{}
			err = errors.New("tenant purge observation failed")
		}
	}()
	return target.Observer.ObservePurgedTenant(ctx, tenant, guard)
}

func validTenantPurgeObservation(target TenantPurgeObservationTarget, tenant uuid.UUID, started time.Time, evidence objectstore.RestorePurgeStorageEvidence) bool {
	id := tenant.String()
	want := [3]string{"tenants/" + id + "/", "staging/tenants/" + id + "/", "originals/tenants/" + id + "/"}
	if evidence.TenantID != tenant || evidence.Bucket != target.Name || evidence.Passes != 2 || len(evidence.Prefixes) != len(want) || evidence.ObservedAt.Before(started) || evidence.ObservedAt.After(time.Now().Add(time.Minute)) {
		return false
	}
	for index, prefix := range want {
		if evidence.Prefixes[index] != prefix {
			return false
		}
	}
	return true
}

// No partial proof escapes: success includes a fresh two-pass observation for
// every installed target, under one caller-provided fixture/writer guard.
func ObservePurgedTenantBuckets(ctx context.Context, tenant uuid.UUID, targets []TenantPurgeObservationTarget, guard func(context.Context, uuid.UUID) error) ([]objectstore.RestorePurgeStorageEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenant == uuid.Nil || guard == nil || len(targets) < 1 || len(targets) > 256 {
		return nil, errors.New("invalid tenant purge observation")
	}
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target.Name == "" || target.Observer == nil || seen[target.Name] {
			return nil, errors.New("invalid tenant purge observation targets")
		}
		seen[target.Name] = true
	}
	scoped, cancel := context.WithCancel(ctx)
	defer cancel()
	started := time.Now().UTC()
	jobs := make(chan int, len(targets))
	results := make(chan tenantPurgeObservationResult, len(targets))
	for index := range targets {
		jobs <- index
	}
	close(jobs)
	workers := len(targets)
	if workers > cap(tenantObservationSlots) {
		workers = cap(tenantObservationSlots)
	}
	for range workers {
		go func() {
			for index := range jobs {
				select {
				case tenantObservationSlots <- struct{}{}:
				case <-scoped.Done():
					return
				}
				if err := scoped.Err(); err != nil {
					<-tenantObservationSlots
					return
				}
				evidence, err := observeTenantPurgeTarget(scoped, targets[index], tenant, guard)
				results <- tenantPurgeObservationResult{index, evidence, err}
			}
		}()
	}
	evidence := make([]objectstore.RestorePurgeStorageEvidence, len(targets))
	for range targets {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-results:
			if result.err != nil {
				return nil, result.err
			}
			if !validTenantPurgeObservation(targets[result.index], tenant, started, result.evidence) {
				return nil, errors.New("invalid tenant purge observation evidence")
			}
			result.evidence.Prefixes = append([]string(nil), result.evidence.Prefixes...)
			evidence[result.index] = result.evidence
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The fixture and writer fence may have changed while the final bucket was
	// being observed. A complete set of bucket results is not authority to
	// publish absence after that fence has been revoked.
	if err := guard(ctx, tenant); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return evidence, nil
}
