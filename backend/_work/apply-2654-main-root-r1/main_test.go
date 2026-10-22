package main

import (
	"strings"
	"testing"
)

func TestMainRunnerRefusesOtherTargetsBeforeConnect(t *testing.T) {
	t.Setenv("CONFIRM_APPLY_2654_MAIN", "main-2653-to-2654-reviewed-locator")
	for _, dsn := range []string{
		"host=127.0.0.1 port=57432 user=rtx_snapshot_admin dbname=postgres",
		"host=127.0.0.1 port=57432 user=rtx_snapshot_admin dbname=photoeditor_apply2654_test",
		"host=192.168.1.3 port=57432 user=rtx_snapshot_admin dbname=photoeditor",
		"host=127.0.0.1 port=5432 user=rtx_snapshot_admin dbname=photoeditor",
		"host=127.0.0.1 port=57432 user=photo_scheduler dbname=photoeditor",
	} {
		t.Run(dsn, func(t *testing.T) {
			t.Setenv("DATABASE_URL", dsn)
			e := run()
			if e == nil || !strings.Contains(e.Error(), "exact MAIN target required") {
				t.Fatalf("not refused before connect: %v", e)
			}
		})
	}
}
func TestMainRunnerRequiresExplicitConfirmation(t *testing.T) {
	t.Setenv("CONFIRM_APPLY_2654_MAIN", "")
	if e := run(); e == nil || e.Error() != "explicit MAIN confirmation required" {
		t.Fatal(e)
	}
	t.Setenv("CONFIRM_APPLY_2654_MAIN", "main-2653-to-2654-reviewed-locator")
	t.Setenv("DATABASE_URL", " ")
	if e := run(); e == nil || e.Error() != "explicit database required" {
		t.Fatal(e)
	}
}
