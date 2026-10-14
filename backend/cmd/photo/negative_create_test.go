package main

import (
	"context"
	"testing"
)

func TestNegativeBootstrapOptionalAndRequiresSchedulerDSN(t *testing.T) {
	t.Setenv("PHOTO_V29_NEGATIVE_CONFIG", "")
	service, closeService, err := buildNegative(context.Background(), nil, "photos")
	if err != nil || service != nil || closeService == nil {
		t.Fatal("disabled negative", err)
	}
	closeService()
	t.Setenv("PHOTO_V29_NEGATIVE_CONFIG", "/not-read-without-scheduler-config")
	t.Setenv("V29_NEGATIVE_SCHEDULER_DATABASE_URL", "")
	if _, _, err := buildNegative(context.Background(), nil, "photos"); err == nil {
		t.Fatal("enabled negative without scheduler DSN")
	}
}
