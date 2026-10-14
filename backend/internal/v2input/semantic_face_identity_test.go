package v2input

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type semanticIdentityDraft interface{ DraftTask() (DraftTask, error) }

func assertSemanticModelIdentity(t *testing.T, original, identified semanticIdentityDraft, model string) {
	t.Helper()
	before, err := original.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	after, err := identified.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	var left, right map[string]any
	if json.Unmarshal(before.ExecutionProfile, &left) != nil || json.Unmarshal(after.ExecutionProfile, &right) != nil {
		t.Fatal("profile")
	}
	for key, value := range map[string]string{"model_lock_id": model, "model_lock_sha": model, "runtime_lock_sha": strings.Repeat("c", 64), "device_profile_sha": strings.Repeat("d", 64)} {
		if _, exists := left[key]; exists {
			t.Fatal("legacy profile mutated")
		}
		if right[key] != value {
			t.Fatalf("missing pin %s", key)
		}
		delete(right, key)
	}
	a, b := left["resource_estimate"].(map[string]any), right["resource_estimate"].(map[string]any)
	if b["scratch_bytes"].(float64)-a["scratch_bytes"].(float64) != 16<<20 {
		t.Fatal("index scratch omitted")
	}
	b["scratch_bytes"] = a["scratch_bytes"]
	if !reflect.DeepEqual(left, right) {
		t.Fatal("unrelated profile properties changed")
	}
	after.ExecutionProfile = before.ExecutionProfile
	if !reflect.DeepEqual(before, after) {
		t.Fatal("identity changed task or closure")
	}
	again, err := original.DraftTask()
	if err != nil || !reflect.DeepEqual(again, before) {
		t.Fatal("aliased source profile")
	}
}
