package semanticreview

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestBatchResultStrictReceiptContract(t *testing.T) {
	good := BatchResult{WorkflowID: uuid.New(), Revision: "2", Remaining: 0, BatchID: uuid.New()}
	for _, child := range []*ChildTicket{nil, {RunID: uuid.New(), State: "queued"}} {
		good.Child = child
		raw, err := canonical.Marshal(good)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeBatchResult(raw); err != nil {
			t.Fatal(err)
		}
		var values map[string]any
		if json.Unmarshal(raw, &values) != nil {
			t.Fatal("fixture")
		}
		for _, key := range []string{"workflow_id", "revision", "remaining", "child", "batch_id"} {
			copyValues := make(map[string]any)
			for k, v := range values {
				copyValues[k] = v
			}
			delete(copyValues, key)
			body, _ := json.Marshal(copyValues)
			if _, err := DecodeBatchResult(body); err == nil {
				t.Fatal("missing required field accepted", key)
			}
		}
	}
	good.Child = &ChildTicket{RunID: uuid.New(), State: "completed"}
	raw, _ := json.Marshal(good)
	if _, err := DecodeBatchResult(raw); err == nil {
		t.Fatal("child completion substituted for admission receipt")
	}
	for _, raw := range []string{`{"workflow_id":null,"revision":"2","remaining":null,"child":null,"batch_id":null}`, `{"workflow_id":"bad"}`, `{"remaining":0,"remaining":1}`} {
		if _, err := DecodeBatchResult([]byte(raw)); err == nil {
			t.Fatal("invalid receipt accepted")
		}
	}
}
