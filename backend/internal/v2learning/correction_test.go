package v2learning

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestCorrectionCreateRequiresExactWireFields(t *testing.T) {
	request := map[string]any{
		"client_event_id": uuid.New().String(), "session_id": uuid.New().String(),
		"asset_version_id": uuid.New().String(), "base_document_id": uuid.New().String(),
		"approved_document_id": uuid.New().String(), "reason": "exposure",
		"learn": false, "person_id": nil, "scene_group_id": nil,
		"consent_id": nil, "consent_epoch": nil,
	}
	decode := func(value map[string]any) error {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var got CorrectionCreate
		if err := json.Unmarshal(body, &got); err != nil {
			return err
		}
		if !validCorrection(got) {
			t.Fatal("valid request rejected")
		}
		return nil
	}
	if err := decode(request); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"learn", "person_id", "consent_epoch"} {
		missing := map[string]any{}
		for name, value := range request {
			if name != key {
				missing[name] = value
			}
		}
		if err := decode(missing); err == nil {
			t.Fatalf("accepted missing %s", key)
		}
	}
	wrongType := map[string]any{}
	for name, value := range request {
		wrongType[name] = value
	}
	wrongType["learn"] = nil
	if err := decode(wrongType); err == nil {
		t.Fatal("accepted null learn")
	}
}
