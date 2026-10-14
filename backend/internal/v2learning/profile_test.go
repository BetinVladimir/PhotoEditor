package v2learning

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestProfileCreateWireAndModePerson(t *testing.T) {
	project, person := uuid.New(), uuid.New()
	cases := []struct {
		body  map[string]any
		valid bool
	}{
		{map[string]any{"project_id": project, "name": "My profile",
			"mode": "photographer", "person_id": nil}, true},
		{map[string]any{"project_id": project, "name": "Person profile",
			"mode": "person", "person_id": person}, true},
		{map[string]any{"project_id": project, "name": "Invalid",
			"mode": "person", "person_id": nil}, false},
		{map[string]any{"project_id": project, "name": "Missing",
			"mode": "photographer"}, false},
	}
	for _, tc := range cases {
		body, err := json.Marshal(tc.body)
		if err != nil {
			t.Fatal(err)
		}
		var req ProfileCreate
		accepted := json.Unmarshal(body, &req) == nil && validProfileCreate(req)
		if accepted != tc.valid {
			t.Fatalf("accepted=%v, expected=%v: %s", accepted, tc.valid, body)
		}
	}
}

func TestProfileCreateRejectsDuplicateFields(t *testing.T) {
	var req ProfileCreate
	if err := json.Unmarshal([]byte(`{"project_id":"00000000-0000-0000-0000-000000000001","name":"first","name":"second","mode":"photographer","person_id":null}`), &req); err == nil {
		t.Fatal("accepted ambiguous profile name")
	}
}
