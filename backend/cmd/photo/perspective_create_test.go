package main

import (
	"context"
	"testing"
)

func TestPerspectiveBootstrapOptionalAndRequiresSchedulerDSN(t *testing.T) {
	t.Setenv("PHOTO_V29_PERSPECTIVE_CONFIG", "")
	service, closeService, err := buildPerspective(context.Background(), nil, "photos")
	if err != nil || service != nil || closeService == nil {
		t.Fatal("disabled perspective", err)
	}
	closeService()
	t.Setenv("PHOTO_V29_PERSPECTIVE_CONFIG", "/not-read-without-scheduler-config")
	t.Setenv("V29_PERSPECTIVE_SCHEDULER_DATABASE_URL", "")
	if _, _, err := buildPerspective(context.Background(), nil, "photos"); err == nil {
		t.Fatal("enabled perspective without scheduler DSN")
	}
}
