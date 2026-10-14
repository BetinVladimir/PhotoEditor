package main

import (
	"context"
	"testing"
	"time"
)

func TestParseOptionsBounds(t *testing.T) {
	options, err := parseOptions([]string{"--timeout=5m", "--poll=3s", "gpu-a"})
	if err != nil || options.node != "gpu-a" || options.timeout != 5*time.Minute || options.poll != 3*time.Second {
		t.Fatal(options, err)
	}
	for _, args := range [][]string{{}, {"a", "b"}, {"--timeout=0s", "a"},
		{"--timeout=3h", "a"}, {"--poll=31s", "a"}} {
		if _, err := parseOptions(args); err == nil {
			t.Fatal("invalid drain options accepted", args)
		}
	}
}

func TestRunRejectsMissingDatabaseBeforeIO(t *testing.T) {
	if err := run(context.Background(), []string{"gpu-a"}, ""); err == nil {
		t.Fatal("missing database accepted")
	}
}
