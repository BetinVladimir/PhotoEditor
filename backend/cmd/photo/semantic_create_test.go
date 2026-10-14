package main

import (
	"context"
	"errors"
	"testing"
)

func TestSemanticCreateOptionalConfigurationAndRequiredInputs(t *testing.T) {
	t.Setenv("PHOTO_V27_CREATE_CONFIG", "")
	t.Setenv("V27_CREATE_SCHEDULER_DATABASE_URL", "")
	t.Setenv("PHOTO_ICC_ROOT", "")
	service, closeService, err := buildSemanticCreate(context.Background(), nil, nil, "")
	if err != nil || service != nil || closeService == nil {
		t.Fatal("absent optional configuration", err)
	}
	closeService()
	t.Setenv("PHOTO_V27_CREATE_CONFIG", "missing-operator-config")
	for _, inputs := range []struct{ dsn, icc string }{{"", ""}, {"postgres://unavailable", ""}, {"", "missing-icc"}} {
		t.Setenv("V27_CREATE_SCHEDULER_DATABASE_URL", inputs.dsn)
		t.Setenv("PHOTO_ICC_ROOT", inputs.icc)
		service, closeService, err := buildSemanticCreate(context.Background(), nil, nil, "")
		if err == nil || service != nil || closeService == nil {
			t.Fatal("configured service silently disabled")
		}
		closeService()
	}
	t.Setenv("V27_CREATE_SCHEDULER_DATABASE_URL", "postgres://unavailable")
	t.Setenv("PHOTO_ICC_ROOT", "missing-icc")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service, closeService, err = buildSemanticCreate(ctx, nil, nil, "")
	if !errors.Is(err, context.Canceled) || service != nil || closeService == nil {
		t.Fatal("cancelled startup reached config IO", err)
	}
	closeService()
}
