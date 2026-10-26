package queue

import (
	"encoding/json"
	"testing"
)

// Actual decoder invocation; producer shape must include finite nonnegative counters.
func TestProgressProducerCountersRequiredAndBounded(t *testing.T) {
	for _, change := range []string{"missing-done", "null-total", "negative", "fraction", "done-over-total"} {
		var m map[string]any
		_ = json.Unmarshal(fixtureEvent("task.progress"), &m)
		metrics := m["metrics"].(map[string]any)
		switch change {
		case "missing-done":
			delete(metrics, "done")
		case "null-total":
			metrics["total"] = nil
		case "negative":
			metrics["done"] = -1
		case "fraction":
			metrics["done"] = 1.5
		case "done-over-total":
			metrics["done"] = 11
		}
		raw, _ := json.Marshal(m)
		if _, e := DecodeOrchestratorEvent(raw, "task.progress", eventFixtureID); e == nil {
			t.Fatal("invalid progress admitted", change)
		}
	}
}

func TestProgressEveryRequiredCounterRejectsNullAndUnsafeInteger(t *testing.T) {
	for _, field := range []string{"done", "total", "duration_ms", "peak_memory_bytes"} {
		for _, value := range []any{nil, int64(9007199254740992)} {
			var m map[string]any
			_ = json.Unmarshal(fixtureEvent("task.progress"), &m)
			m["metrics"].(map[string]any)[field] = value
			raw, _ := json.Marshal(m)
			if _, e := DecodeOrchestratorEvent(raw, "task.progress", eventFixtureID); e == nil {
				t.Fatal("bad progress counter admitted", field)
			}
		}
	}
}
