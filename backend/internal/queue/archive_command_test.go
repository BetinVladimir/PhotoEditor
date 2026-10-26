package queue

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func largeArchiveCommand(t *testing.T) map[string]any {
	inputs := make([]map[string]string, 5000)
	for i := range inputs {
		inputs[i] = map[string]string{"artifact_id": uuid.New().String(), "sha256": strings.Repeat("a", 64)}
	}
	return map[string]any{"schema_version": 1, "event_id": uuid.New().String(), "tenant_id": uuid.New().String(), "run_id": uuid.New().String(), "task_id": uuid.New().String(), "attempt": 1, "operation": "archive.build", "operation_version": "1", "resource_class": "archive", "correlation_id": uuid.New().String(), "inputs": inputs, "params": map[string]string{"artifact_id": uuid.New().String(), "sha256": strings.Repeat("b", 64)}, "model": nil, "timeout_seconds": 7200, "traceparent": ""}
}
func archiveBody(t *testing.T, v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestFull5000ArchiveCommandOnlyLargeTransport(t *testing.T) {
	v := largeArchiveCommand(t)
	b := archiveBody(t, v)
	if len(b) <= 65536 || len(b) > 1048576 || !taskEnvelopePublishable(Commands, b) || !restoreTaskEnvelopePublishable(Commands, b) {
		t.Fatal("full5000 normalcommand notpublishable", len(b))
	}
	if taskEnvelopePublishable(Events, b) {
		t.Fatal("eventlimit broadened")
	}
	for key, value := range map[string]any{"operation": "image.local", "resource_class": "cpu", "operation_version": "2", "timeout_seconds": 7201, "attempt": 7} {
		old := v[key]
		v[key] = value
		if archiveCommandPayload(archiveBody(t, v)) {
			t.Fatal("wrongclosedcommand admitted", key)
		}
		v[key] = old
	}
}
func TestLargeArchiveRoutingAndEventIdentity(t *testing.T) {
	v := largeArchiveCommand(t)
	b := archiveBody(t, v)
	event := uuid.MustParse(v["event_id"].(string))
	routing := "worker." + uuid.NewString() + ".io.archive"
	if !archiveEnvelopeIdentity(routing, event, b) {
		t.Fatal("ownarchive route refused")
	}
	for _, r := range []string{"io.cpu", "worker." + uuid.NewString() + ".io.media", "worker.invalid.io.archive"} {
		if archiveEnvelopeIdentity(r, event, b) {
			t.Fatal("foreignroute accepted")
		}
	}
	if archiveEnvelopeIdentity(routing, uuid.New(), b) {
		t.Fatal("eventmismatch accepted")
	}
}
func TestArchiveCommandCountAndUnknownFields(t *testing.T) {
	v := largeArchiveCommand(t)
	inputs := v["inputs"].([]map[string]string)
	v["inputs"] = append(inputs, map[string]string{"artifact_id": uuid.NewString(), "sha256": strings.Repeat("c", 64)})
	if archiveCommandPayload(archiveBody(t, v)) {
		t.Fatal("5001accepted")
	}
	v["inputs"] = inputs
	v["secret_extra"] = "notallowed"
	if archiveCommandPayload(archiveBody(t, v)) {
		t.Fatal("unknownfieldaccepted")
	}
}
func TestLargeArchiveDeadLetterValidOrDigestOnly(t *testing.T) {
	v := largeArchiveCommand(t)
	b := archiveBody(t, v)
	record, e := DecodeDeadLetter(b, "dead.archive")
	if e != nil || record.TaskID == nil || record.WorkerClass != "archive" {
		t.Fatal(record, e)
	}
	v["operation"] = "image.local"
	record, e = DecodeDeadLetter(archiveBody(t, v), "dead.archive")
	if e != nil || record.TaskID != nil || record.EventID != nil || !strings.Contains(string(record.Metadata), "invalid_command") {
		t.Fatal("poisonnotquarantined", record, e)
	}
	if _, e = DecodeDeadLetter(b, "dead.cpu"); e == nil {
		t.Fatal("otherclasslimitbroadened")
	}
}

func TestLargeArchivePreservesRepeatedContentRefContract(t *testing.T) {
	v := largeArchiveCommand(t)
	refs := v["inputs"].([]map[string]string)
	refs[1] = refs[0]
	v["inputs"] = refs
	if !archiveCommandPayload(archiveBody(t, v)) {
		t.Fatal("existingrepeatedrefcontractnarrowed")
	}
}
