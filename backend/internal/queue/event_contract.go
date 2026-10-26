package queue

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
)

type OrchestratorEvent struct {
	EventID, TenantID, RunID uuid.UUID
	Type                     string
	Payload                  json.RawMessage
}

func eventFields(m map[string]json.RawMessage, required, optional string) bool {
	allowed := map[string]bool{}
	for _, k := range strings.Fields(required) {
		allowed[k] = true
		if _, ok := m[k]; !ok {
			return false
		}
	}
	for _, k := range strings.Fields(optional) {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return false
		}
	}
	return true
}
func eventUUID(m map[string]json.RawMessage, k string) uuid.UUID {
	var s string
	if json.Unmarshal(m[k], &s) != nil {
		return uuid.Nil
	}
	v, _ := uuid.Parse(s)
	return v
}
func eventInteger(raw json.RawMessage, min int64) bool {
	var v int64
	return string(raw) != "null" && json.Unmarshal(raw, &v) == nil && v >= min
}
func eventString(raw json.RawMessage, max int) string {
	var v string
	if json.Unmarshal(raw, &v) != nil || len(v) > max {
		return ""
	}
	return v
}

// DecodeOrchestratorEvent accepts actual durable SQL producer envelopes, never commands.
func DecodeOrchestratorEvent(raw []byte, routing, messageID string) (OrchestratorEvent, error) {
	bad := errors.New("invalid orchestrator event")
	var e OrchestratorEvent
	if len(raw) == 0 || len(raw) > 65536 {
		return e, bad
	}
	b, err := canonical.Bytes(raw)
	if err != nil {
		return e, bad
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil || m == nil {
		return e, bad
	}
	e.EventID = eventUUID(m, "event_id")
	e.TenantID = eventUUID(m, "tenant_id")
	e.RunID = eventUUID(m, "run_id")
	e.Type = eventString(m["type"], 80)
	if string(m["schema_version"]) != "1" || e.EventID == uuid.Nil || e.TenantID == uuid.Nil || e.RunID == uuid.Nil || e.Type != routing || messageID != e.EventID.String() {
		return e, bad
	}
	common := "schema_version event_id type tenant_id run_id "
	switch e.Type {
	case "task.succeeded", "task.failed", "task.cancelled", "task.progress":
		if !eventFields(m, common+"task_id attempt fencing_token outputs metrics error", "") || eventUUID(m, "task_id") == uuid.Nil || !eventInteger(m["attempt"], 1) || !eventInteger(m["fencing_token"], 1) {
			return e, bad
		}
		var outputs []map[string]json.RawMessage
		if json.Unmarshal(m["outputs"], &outputs) != nil || string(m["outputs"]) == "null" || len(outputs) > 128 {
			return e, bad
		}
		for _, o := range outputs {
			sha := eventString(o["sha256"], 64)
			if !eventFields(o, "artifact_id sha256", "") || eventUUID(o, "artifact_id") == uuid.Nil || len(sha) != 64 || strings.Trim(sha, "0123456789abcdef") != "" {
				return e, bad
			}
		}
		if e.Type != "task.succeeded" && len(outputs) > 0 {
			return e, bad
		}
		var metrics map[string]json.RawMessage
		requiredMetrics := ""
		if e.Type == "task.progress" {
			requiredMetrics = "duration_ms peak_memory_bytes done total"
		}
		if json.Unmarshal(m["metrics"], &metrics) != nil || metrics == nil || !eventFields(metrics, requiredMetrics, "duration_ms peak_memory_bytes peak_device_bytes done total bytes_read bytes_written") {
			return e, bad
		}
		for k, v := range metrics {
			if string(v) == "null" && k != "duration_ms" && k != "peak_memory_bytes" && !(e.Type == "task.progress" && (k == "done" || k == "total")) {
				continue
			}
			if !eventInteger(v, 0) {
				return e, bad
			}
			if e.Type == "task.progress" {
				var count int64
				if json.Unmarshal(v, &count) != nil || count > 9007199254740991 {
					return e, bad
				}
			}
		}
		if metrics["done"] != nil && metrics["total"] != nil && string(metrics["done"]) != "null" && string(metrics["total"]) != "null" {
			var d, t int64
			json.Unmarshal(metrics["done"], &d)
			json.Unmarshal(metrics["total"], &t)
			if d > t {
				return e, bad
			}
		}
		if e.Type == "task.succeeded" || e.Type == "task.progress" {
			if string(m["error"]) != "null" {
				return e, bad
			}
		} else if string(m["error"]) != "null" {
			var x map[string]json.RawMessage
			if json.Unmarshal(m["error"], &x) != nil || !eventFields(x, "code retryable", "message_key args diagnostic_artifact_id") || eventString(x["code"], 120) == "" {
				return e, bad
			}
			var retry bool
			if json.Unmarshal(x["retryable"], &retry) != nil || string(x["retryable"]) == "null" {
				return e, bad
			}
			if key, ok := x["message_key"]; ok && eventString(key, 256) == "" {
				return e, bad
			}
			if rawArgs, ok := x["args"]; ok {
				var args map[string]any
				if json.Unmarshal(rawArgs, &args) != nil || args == nil {
					return e, bad
				}
				for _, v := range args {
					switch v.(type) {
					case nil, string, float64, bool:
					default:
						return e, bad
					}
				}
			}
			if v, ok := x["diagnostic_artifact_id"]; ok && string(v) != "null" && eventUUID(x, "diagnostic_artifact_id") == uuid.Nil {
				return e, bad
			}
		} else if e.Type == "task.failed" {
			return e, bad
		}
	case "download_status":
		if !eventFields(m, common+"download_job_id status", "") || eventUUID(m, "download_job_id") == uuid.Nil {
			return e, bad
		}
		switch eventString(m["status"], 20) {
		case "ready", "failed", "cancelled", "expired":
		default:
			return e, bad
		}
	case "delivery.cancelled":
		if !eventFields(m, common+"package_id status", "") || eventUUID(m, "package_id") == uuid.Nil || eventString(m["status"], 20) != "cancelled" {
			return e, bad
		}
	case "review.created":
		if !eventFields(m, common+"review_id kind", "") || eventUUID(m, "review_id") == uuid.Nil || eventString(m["kind"], 120) == "" {
			return e, bad
		}
	default:
		return e, bad
	}
	e.Payload = b
	return e, nil
}
