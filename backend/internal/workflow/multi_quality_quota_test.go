package workflow

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestMultiQualityQuotaPreflightAndCancellation(t *testing.T) {
	store := MultiQualityIntentStore{}
	if MultiQualityReservedBytes != 2097152 {
		t.Fatal("quality budget drift")
	}
	if ok, err := store.Reserve(context.Background(), MultiQualityPlan{}); ok || err == nil {
		t.Fatal("unverified reservation")
	}
	if ok, err := store.Release(context.Background(), uuid.New(), uuid.New()); ok || err == nil {
		t.Fatal("missing DB release")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Reserve(ctx, MultiQualityPlan{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := store.Release(ctx, uuid.New(), uuid.New()); err != context.Canceled {
		t.Fatal(err)
	}
}
