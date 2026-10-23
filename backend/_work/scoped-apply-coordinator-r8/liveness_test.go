package main

import (
	"encoding/json"
	"testing"
)

func normalizeRow(t *testing.T, table, raw string) string {
	t.Helper()
	var row map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatal(err)
	}
	skip, err := syntheticScope().normalize(table, row)
	if err != nil || skip {
		t.Fatalf("unexpected exemption/error: %v %v", skip, err)
	}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func TestLivenessTelemetryOnly(t *testing.T) {
	for _, tc := range []struct{ table, a, b string }{
		{"worker_registrations", `{"id":"w","last_seen":"a","capacity_policy_reported_at":"a","model_residency":[],"model_residency_reported_at":"a"}`, `{"id":"w","last_seen":"b","capacity_policy_reported_at":"b","model_residency":["loaded"],"model_residency_reported_at":"b"}`},
		{"resource_nodes", `{"node_id":"n","reported_at":"a","ram_available_bytes":1,"vram_available_bytes":2,"scratch_available_bytes":3}`, `{"node_id":"n","reported_at":"b","ram_available_bytes":4,"vram_available_bytes":5,"scratch_available_bytes":6}`},
	} {
		if normalizeRow(t, tc.table, tc.a) != normalizeRow(t, tc.table, tc.b) {
			t.Fatalf("telemetry not normalized: %s", tc.table)
		}
	}
}
func TestLivenessAuthorityDriftProtected(t *testing.T) {
	for _, table := range []string{"worker_registrations", "resource_nodes"} {
		for _, field := range []string{"id", "worker_id", "tenant_id", "node_id", "enabled", "db_principal", "certificate_sha256", "lock_id", "resource_classes", "backends", "capacity", "qualified_operations", "capacity_policy_sha", "reported_capacity_policy_sha", "ram_budget_bytes", "vram_budget_bytes", "scratch_budget_bytes", "doctor_report_sha256", "unknown_future_authority"} {
			before := normalizeRow(t, table, `{"`+field+`":"old"}`)
			after := normalizeRow(t, table, `{"`+field+`":"new"}`)
			if before == after {
				t.Fatalf("authority drift hidden: %s.%s", table, field)
			}
		}
	}
}
func TestLivenessDoesNotNormalizeOtherTables(t *testing.T) {
	if normalizeRow(t, "artifacts", `{"last_seen":"a"}`) == normalizeRow(t, "artifacts", `{"last_seen":"b"}`) {
		t.Fatal("unrelated table exempted")
	}
}
