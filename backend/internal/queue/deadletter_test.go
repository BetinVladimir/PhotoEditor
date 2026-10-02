package queue

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDecodeDeadLetterKeepsOnlyAllowlistedProjection(t *testing.T) {
	task, event := uuid.New(), uuid.New()
	raw := `{"schema_version":1,"event_id":"` + event.String() + `","tenant_id":"` + uuid.NewString() + `","run_id":"` + uuid.NewString() + `","task_id":"` + task.String() + `","attempt":2,"operation":"asset.verify","operation_version":"1","resource_class":"media","correlation_id":"` + uuid.NewString() + `","inputs":[],"params":{},"model":null,"timeout_seconds":60,"traceparent":""}`
	record, err := DecodeDeadLetter([]byte(raw), "dead.media")
	if err != nil || record.TaskID == nil || *record.TaskID != task || record.EventID == nil || *record.EventID != event || strings.Contains(string(record.Metadata), "secret") {
		t.Fatal(record, err)
	}
}

func TestDecodeDeadLetterRejectsUnknownSensitiveFields(t *testing.T) {
	record, err := DecodeDeadLetter([]byte(`{"schema_version":1,"token":"secret"}`), "dead.media")
	if err != nil || record.TaskID != nil || strings.Contains(string(record.Metadata), "secret") || !strings.Contains(string(record.Metadata), "invalid_command") {
		t.Fatal(record, err)
	}
}

func TestDecodeDeadLetterStoresOnlyDigestForMalformedPayload(t *testing.T) {
	record, err := DecodeDeadLetter([]byte(`{"token":"secret"}`), "dead.cpu")
	if err != nil || record.TaskID != nil || strings.Contains(string(record.Metadata), "secret") || !strings.Contains(string(record.Metadata), "invalid_command") {
		t.Fatal(record, err)
	}
	if len(record.PayloadSHA256) != 64 {
		t.Fatal("missing digest")
	}
}
