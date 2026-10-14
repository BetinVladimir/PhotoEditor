package semanticreview

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestCloseResponseCannotCarryChildOrIncompleteStatus(t *testing.T) {
	valid := map[string]any{"run_id": uuid.New().String(), "workflow_id": uuid.New().String(), "revision": "2", "state": "completed", "outputs": []any{}, "error_code": nil}
	raw, _ := json.Marshal(valid)
	if _, err := DecodeCloseResult(raw); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(map[string]any){
		func(v map[string]any) { delete(v, "error_code") },
		func(v map[string]any) { v["outputs"] = nil },
		func(v map[string]any) { v["outputs"] = []any{map[string]any{"artifact_id": uuid.New().String()}} },
		func(v map[string]any) { v["state"] = "waiting_review" },
		func(v map[string]any) { v["revision"] = "9223372036854775808" },
		func(v map[string]any) { v["child"] = uuid.New().String() },
	} {
		var changed map[string]any
		_ = json.Unmarshal(raw, &changed)
		mutation(changed)
		bad, _ := json.Marshal(changed)
		if _, err := DecodeCloseResult(bad); err == nil {
			t.Fatal("invalid closure accepted", string(bad))
		}
	}
}
