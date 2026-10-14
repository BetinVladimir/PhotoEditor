package main

import (
	"context"
	"errors"
	"testing"
)

func TestSigningReplaySQLPreflightRefusesMissingDatabaseAndCancellation(t *testing.T) {
	if err := verifySigningReplaySQL(context.Background(), nil); err == nil {
		t.Fatal("missing signing replay database accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifySigningReplaySQL(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled signing replay preflight reached database", err)
	}
}
