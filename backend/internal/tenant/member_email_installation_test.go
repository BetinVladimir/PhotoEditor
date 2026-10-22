package tenant

import (
	"context"
	"errors"
	"testing"
)

func TestMemberEmailInstallationBoundary(t *testing.T) {
	if RequireMemberEmailInstallation(context.Background(), nil) == nil {
		t.Fatal("nil accepted")
	}
	d := &adminInstallationDB{}
	if RequireMemberEmailInstallation(context.Background(), d) == nil {
		t.Fatal("drift accepted")
	}
	d.ready = true
	if err := RequireMemberEmailInstallation(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("query failed")
	d.err = failure
	if !errors.Is(RequireMemberEmailInstallation(context.Background(), d), failure) {
		t.Fatal("query error lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.calls = 0
	if !errors.Is(RequireMemberEmailInstallation(ctx, d), context.Canceled) || d.calls != 0 {
		t.Fatal("cancelled query executed")
	}
	d.err = nil
	ctx, cancel = context.WithCancel(context.Background())
	d.cancel = cancel
	if !errors.Is(RequireMemberEmailInstallation(ctx, d), context.Canceled) {
		t.Fatal("late readiness accepted")
	}
}
