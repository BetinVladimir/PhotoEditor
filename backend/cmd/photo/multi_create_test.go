package main

import (
	"context"
	"testing"
)

func TestMultiBootstrapOptionalAndOperationSpecific(t *testing.T) {
	for _, sample := range []struct{ operation, stem string }{{"optical.panorama", "PANORAMA"}, {"optical.focus_stack", "FOCUS_STACK"}} {
		t.Run(sample.stem, func(t *testing.T) {
			t.Setenv("PHOTO_V29_"+sample.stem+"_CONFIG", "")
			service, closeService, err := buildMulti(context.Background(), nil, "photos", sample.operation)
			if err != nil || service != nil || closeService == nil {
				t.Fatal("disabled operation", err)
			}
			closeService()
			t.Setenv("PHOTO_V29_"+sample.stem+"_CONFIG", "/not-read-without-scheduler-config")
			t.Setenv("V29_"+sample.stem+"_SCHEDULER_DATABASE_URL", "")
			if _, _, err := buildMulti(context.Background(), nil, "photos", sample.operation); err == nil {
				t.Fatal("enabled operation without scheduler DSN")
			}
		})
	}
	if _, _, err := buildMulti(context.Background(), nil, "photos", "optical.perspective"); err == nil {
		t.Fatal("foreign operation admitted")
	}
}
