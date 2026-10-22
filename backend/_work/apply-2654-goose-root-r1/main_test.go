package main

import (
	"strings"
	"testing"
)

func TestOwnedCloneRunnerRefusesMainAndOtherTargets(t *testing.T) {
	t.Setenv("CONFIRM_APPLY_2654_CLONE", "owned-clone-2653-to-2654")
	for _, dsn := range []string{
		"host=127.0.0.1 port=57432 user=rtx_snapshot_admin dbname=photoeditor",
		"host=127.0.0.1 port=57432 user=rtx_snapshot_admin dbname=postgres",
		"host=192.168.1.3 port=57432 user=rtx_snapshot_admin dbname=photoeditor_apply2654_test",
		"host=127.0.0.1 port=5432 user=rtx_snapshot_admin dbname=photoeditor_apply2654_test",
		"host=127.0.0.1 port=57432 user=photo_scheduler dbname=photoeditor_apply2654_test",
		"host=127.0.0.1 port=57432 user=rtx_snapshot_admin dbname=photoeditor_apply2654_",
	} {
		t.Run(dsn, func(t *testing.T) {
			t.Setenv("DATABASE_URL", dsn)
			e := run()
			if e == nil || !strings.Contains(e.Error(), "owned clone only") {
				t.Fatalf("target not refused before connect: %v", e)
			}
		})
	}
}
func TestOwnedCloneRunnerRequiresExplicitConfiguration(t *testing.T) {
	t.Setenv("CONFIRM_APPLY_2654_CLONE", "")
	t.Setenv("DATABASE_URL", "")
	if e := run(); e == nil || e.Error() != "explicit owned clone confirmation required" {
		t.Fatal(e)
	}
	t.Setenv("CONFIRM_APPLY_2654_CLONE", "owned-clone-2653-to-2654")
	t.Setenv("DATABASE_URL", "  ")
	if e := run(); e == nil || e.Error() != "explicit database required" {
		t.Fatal(e)
	}
}
