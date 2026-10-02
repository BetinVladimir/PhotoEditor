package collection

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeBulkActions(t *testing.T) {
	snapshot := uuid.New()
	target := uuid.New()
	cases := []BulkRequest{
		{1, snapshot, "rating", json.RawMessage(`{"rating":5}`)},
		{1, snapshot, "flag", json.RawMessage(`{"flag":"pick"}`)},
		{1, snapshot, "tag", json.RawMessage(`{"mode":"add","tag":"portrait"}`)},
		{1, snapshot, "hide", json.RawMessage(`{}`)},
		{1, snapshot, "publish", json.RawMessage(`{"publish":true}`)},
		{1, snapshot, "copy", json.RawMessage(`{"target_collection_id":"` + target.String() + `"}`)},
		{1, snapshot, "retry_media", json.RawMessage(`{"variants":["thumbnail","preview"]}`)},
	}
	for _, request := range cases {
		if _, _, err := normalizeBulk(request); err != nil {
			t.Fatalf("%s: %v", request.Action, err)
		}
	}
}

func TestNormalizeBulkRejectsUnknownOrAmbiguousParams(t *testing.T) {
	snapshot := uuid.New()
	cases := []BulkRequest{
		{0, snapshot, "hide", json.RawMessage(`{}`)},
		{1, snapshot, "rating", json.RawMessage(`{"rating":6}`)},
		{1, snapshot, "hide", json.RawMessage(`{"extra":true}`)},
		{1, snapshot, "tag", json.RawMessage(`{"mode":"replace","tag":"x"}`)},
		{1, snapshot, "retry_media", json.RawMessage(`{"variants":["thumbnail","thumbnail"]}`)},
		{1, snapshot, "unknown", json.RawMessage(`{}`)},
	}
	for _, request := range cases {
		if _, _, err := normalizeBulk(request); err == nil {
			t.Fatalf("accepted %#v", request)
		}
	}
}
