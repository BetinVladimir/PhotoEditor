package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"reflect"
	"testing"
)

func assertSemanticSkinIndexBudget(t *testing.T, original, indexed FrozenSemanticFaceSkinTask) {
	t.Helper()
	before, err := original.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	after, err := indexed.DraftTask()
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	if json.Unmarshal(before.ExecutionProfile, &a) != nil || json.Unmarshal(after.ExecutionProfile, &b) != nil {
		t.Fatal("profiles")
	}
	if _, present := a["input_index_scratch_bytes"]; present || b["input_index_scratch_bytes"] != float64(16<<20) {
		t.Fatal("index marker")
	}
	delete(b, "input_index_scratch_bytes")
	left, right := a["resource_estimate"].(map[string]any), b["resource_estimate"].(map[string]any)
	if right["scratch_bytes"].(float64)-left["scratch_bytes"].(float64) != 16<<20 {
		t.Fatal("concurrent index scratch missing")
	}
	right["scratch_bytes"] = left["scratch_bytes"]
	if !reflect.DeepEqual(a, b) {
		t.Fatal("unrelated profile changed")
	}
	after.ExecutionProfile = before.ExecutionProfile
	if !reflect.DeepEqual(before, after) {
		t.Fatal("Params/task/closure changed")
	}
	again, err := original.DraftTask()
	if err != nil || !reflect.DeepEqual(again, before) {
		t.Fatal("original profile aliased")
	}
}
func TestSkinInputIndexBudgetRejectsInvalidProfiles(t *testing.T) {
	for _, scratch := range []any{-1, .5, float64(9007199254740991 - (16 << 20) + 1), "1024", nil} {
		raw, _ := canonical.Marshal(map[string]any{"backend": "cpu", "renderer_version": "v2.2", "resource_estimate": map[string]any{"scratch_bytes": scratch}})
		result, err := (FrozenSemanticFaceSkinTask{profile: string(raw)}).WithInputIndexScratch()
		if err == nil || !reflect.DeepEqual(result, FrozenSemanticFaceSkinTask{}) {
			t.Fatal("invalid skin budget accepted", scratch)
		}
	}
	for _, raw := range []string{`null`, `{}`, `{"backend":"cuda","renderer_version":"v2.2"}`, `{"backend":"cpu","renderer_version":"v2.2","input_index_scratch_bytes":0}`, `{"backend":"cpu","renderer_version":"v2.2","resource_estimate":[]}`} {
		if _, err := (FrozenSemanticFaceSkinTask{profile: raw}).WithInputIndexScratch(); err == nil {
			t.Fatal("invalid profile accepted", raw)
		}
	}
}
