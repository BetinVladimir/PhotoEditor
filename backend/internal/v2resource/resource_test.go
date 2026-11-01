package v2resource

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestID_valid(t *testing.T) {
	id := uuid.New()
	raw, _ := json.Marshal(map[string]any{"id": id})
	got, err := ID(raw)
	if err != nil || got != id {
		t.Fatalf("ID(%s) = %v, %v; want %s, nil", raw, got, err, id)
	}
}

func TestID_nilUUID(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"id": uuid.Nil})
	if _, err := ID(raw); err == nil {
		t.Fatal("nil UUID must return error")
	}
}

func TestID_malformedJSON(t *testing.T) {
	if _, err := ID([]byte(`{not json`)); err == nil {
		t.Fatal("malformed JSON must return error")
	}
}

func TestID_missingIDField(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"other": "value"})
	if _, err := ID(raw); err == nil {
		t.Fatal("missing id field must return error")
	}
}

func TestLoad_unknownKind(t *testing.T) {
	// Load with an unknown resource kind returns an error before any DB call.
	_, err := Load(nil, nil, uuid.New(), uuid.New(), "unknown_kind")
	if err == nil || err.Error() != "unknown V2 resource kind" {
		t.Fatalf("unknown kind must return sentinel error, got %v", err)
	}
}

