package v2learning

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTrainCreateRejectsUnboundedOrAmbiguousInput(t *testing.T) {
	sessions := make([]uuid.UUID, 6)
	for i := range sessions {
		sessions[i] = uuid.New()
	}
	body := map[string]any{
		"expected_revision": "1", "session_ids": sessions,
		"policy": map[string]any{
			"schema_version": 1, "mode": "photographer",
			"min_examples": 40, "min_sessions": 6,
			"holdout_fraction": 0.2, "seed": 2026,
			"calibration": map[string]any{
				"artifact_id": uuid.New(), "sha256": strings.Repeat("a", 64),
			},
		},
	}
	check := func(value map[string]any) bool {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var req TrainCreate
		if json.Unmarshal(raw, &req) != nil {
			return false
		}
		_, err = ValidateTrainCreate(req)
		return err == nil
	}
	if !check(body) {
		t.Fatal("valid training request rejected")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, ambiguous := range []string{
		strings.Replace(string(raw), `"schema_version":1`, `"schema_version":1,"schema_version":2`, 1),
		strings.Replace(string(raw), `"sha256":"`+strings.Repeat("a", 64)+`"`, `"sha256":"`+strings.Repeat("a", 64)+`","sha256":"`+strings.Repeat("b", 64)+`"`, 1),
	} {
		var req TrainCreate
		if json.Unmarshal([]byte(ambiguous), &req) == nil {
			t.Fatal("accepted duplicate nested training field")
		}
	}
	wrong := map[string]any{}
	for key, value := range body {
		wrong[key] = value
	}
	wrong["session_ids"] = []uuid.UUID{sessions[0], sessions[1], sessions[2],
		sessions[3], sessions[4], sessions[4]}
	if check(wrong) {
		t.Fatal("duplicate session accepted")
	}
	wrong["session_ids"] = sessions
	wrong["expected_revision"] = "01"
	if check(wrong) {
		t.Fatal("noncanonical revision accepted")
	}
	wrong["expected_revision"] = "1"
	wrong["policy"] = map[string]any{
		"schema_version": 1, "mode": "photographer", "min_examples": 40,
		"min_sessions": 6, "holdout_fraction": 0.3, "seed": 2026,
		"calibration": body["policy"].(map[string]any)["calibration"],
	}
	if check(wrong) {
		t.Fatal("unapproved holdout split accepted")
	}
	delete(wrong, "policy")
	if check(wrong) {
		t.Fatal("missing policy accepted")
	}
}
