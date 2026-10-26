package queue

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/archivecontract"
	"photoeditor/backend/internal/canonical"
	"strings"
)

// The larger transport is operation-scoped, not a new general queue limit.
func taskEnvelopePublishable(destination string, raw []byte) bool {
	if destination != Commands && destination != Events {
		return false
	}
	if len(raw) <= 65536 {
		return true
	}
	return destination == Commands && archiveCommandPayload(raw)
}
func archiveCommandPayload(raw []byte) bool {
	if len(raw) == 0 || len(raw) > archivecontract.CommandBytes {
		return false
	}
	if _, err := canonical.Bytes(raw); err != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	allowed := []string{"schema_version", "event_id", "tenant_id", "run_id", "task_id", "attempt", "operation", "operation_version", "resource_class", "correlation_id", "inputs", "params", "model", "timeout_seconds", "traceparent"}
	if len(fields) != len(allowed) {
		return false
	}
	for _, key := range allowed {
		if _, ok := fields[key]; !ok {
			return false
		}
	}
	var command struct {
		Schema    int               `json:"schema_version"`
		Attempt   int               `json:"attempt"`
		Timeout   int               `json:"timeout_seconds"`
		Operation string            `json:"operation"`
		Version   string            `json:"operation_version"`
		Class     string            `json:"resource_class"`
		Trace     string            `json:"traceparent"`
		Inputs    []json.RawMessage `json:"inputs"`
	}
	if json.Unmarshal(raw, &command) != nil || command.Schema != 1 || command.Attempt < 1 || command.Attempt > 6 || command.Timeout != 7200 || command.Operation != "archive.build" || command.Version != "1" || command.Class != "archive" || len(command.Trace) > 128 || len(command.Inputs) < 1 || len(command.Inputs) > archivecontract.MaxEntries || !bytes.Equal(bytes.TrimSpace(fields["model"]), []byte("null")) {
		return false
	}
	for _, key := range []string{"event_id", "tenant_id", "run_id", "task_id", "correlation_id"} {
		var value string
		if json.Unmarshal(fields[key], &value) != nil {
			return false
		}
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return false
		}
	}
	if _, ok := archiveRef(fields["params"]); !ok {
		return false
	}
	// Ref repetition is permitted by existing typed contracts. Entry-name and
	// ordinal uniqueness belong to ArchiveParams, not transport identity.
	for _, rawRef := range command.Inputs {
		if _, ok := archiveRef(rawRef); !ok {
			return false
		}
	}
	return true
}
func archiveRef(raw json.RawMessage) (uuid.UUID, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 {
		return uuid.Nil, false
	}
	var value, hash string
	if json.Unmarshal(fields["artifact_id"], &value) != nil || json.Unmarshal(fields["sha256"], &hash) != nil || len(hash) != 64 || strings.ToLower(hash) != hash {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return uuid.Nil, false
	}
	decoded, err := hex.DecodeString(hash)
	return id, err == nil && len(decoded) == 32
}

func archiveEnvelopeIdentity(routing string, event uuid.UUID, raw []byte) bool {
	if routing != "io.archive" {
		parts := strings.Split(routing, ".")
		if len(parts) != 4 || parts[0] != "worker" || parts[2] != "io" || parts[3] != "archive" {
			return false
		}
		worker, err := uuid.Parse(parts[1])
		if err != nil || worker == uuid.Nil || worker.String() != parts[1] {
			return false
		}
	}
	var body struct {
		Event uuid.UUID `json:"event_id"`
	}
	return event != uuid.Nil && json.Unmarshal(raw, &body) == nil && body.Event == event
}
