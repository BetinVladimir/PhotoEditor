package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

type nativeRecoveryRuntimeTestJob struct{}

func (nativeRecoveryRuntimeTestJob) ProcessOne(context.Context) (bool, error) { return false, nil }

func TestNativeRecoveryFactoryClosesFailedStartupAndHonorsCancellation(t *testing.T) {
	failure := errors.New("qualified policy unavailable")
	for _, which := range []string{"failure", "missing_job", "missing_cleanup", "cancelled"} {
		t.Run(which, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			closes := 0
			factory := func(context.Context, *pgxpool.Pool, *objectstore.Store, string) (ExpansionJob, func(), error) {
				var job ExpansionJob = nativeRecoveryRuntimeTestJob{}
				var closeFn func() = func() { closes++ }
				var err error
				switch which {
				case "failure":
					err = failure
				case "missing_job":
					job = nil
				case "missing_cleanup":
					closeFn = nil
				case "cancelled":
					cancel()
				}
				return job, closeFn, err
			}
			job, closeFn, err := newSchedulerNativeRecovery(ctx, factory, nil, nil, "")
			if err == nil || job != nil || closeFn != nil {
				t.Fatal("incomplete startup exposed processor", err)
			}
			expected := 1
			if which == "missing_cleanup" {
				expected = 0
			}
			if closes != expected {
				t.Fatal("factory resources leaked or closed twice", closes)
			}
			if which == "failure" && !errors.Is(err, failure) {
				t.Fatal(err)
			}
			if which == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeRecoveryFactoryOptionalAndOwnedLifetime(t *testing.T) {
	if job, closeFn, err := newSchedulerNativeRecovery(context.Background(), nil, nil, nil, ""); job != nil || closeFn != nil || err != nil {
		t.Fatal("unconfigured factory changed startup", err)
	}
	closes := 0
	factory := func(context.Context, *pgxpool.Pool, *objectstore.Store, string) (ExpansionJob, func(), error) {
		return nativeRecoveryRuntimeTestJob{}, func() { closes++ }, nil
	}
	job, closeFn, err := newSchedulerNativeRecovery(context.Background(), factory, nil, nil, "")
	if err != nil || job == nil || closeFn == nil || closes != 0 {
		t.Fatal("valid factory rejected or closed before loop", err)
	}
	closeFn()
	if closes != 1 {
		t.Fatal("owned resources not closed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	blocked := func(context.Context, *pgxpool.Pool, *objectstore.Store, string) (ExpansionJob, func(), error) {
		called = true
		return nil, nil, nil
	}
	if _, _, err := newSchedulerNativeRecovery(ctx, blocked, nil, nil, ""); err != context.Canceled || called {
		t.Fatal("cancelled startup invoked factory", err)
	}
}

func TestNativeRecoveryInstallationRefusesAbsentPoolBeforeWork(t *testing.T) {
	if err := verifyNativeRecoveryInstallation(context.Background(), nil); err == nil {
		t.Fatal("absent scheduler accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyNativeRecoveryInstallation(ctx, nil); err != context.Canceled {
		t.Fatal(err)
	}
}
