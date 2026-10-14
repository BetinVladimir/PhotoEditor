package semanticreview

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
)

type ChildTicket struct {
	RunID uuid.UUID `json:"run_id"`
	State string    `json:"state"`
}
type BatchResult struct {
	WorkflowID uuid.UUID    `json:"workflow_id"`
	Revision   string       `json:"revision"`
	Remaining  int          `json:"remaining"`
	Child      *ChildTicket `json:"child"`
	BatchID    uuid.UUID    `json:"batch_id"`
}

func DecodeBatchResult(raw []byte) (BatchResult, error) {
	var result BatchResult
	if decodeStrict(raw, &result) != nil || result.WorkflowID == uuid.Nil || result.BatchID == uuid.Nil || !validRevision(result.Revision) || result.Remaining < 0 || result.Remaining > 5000 {
		return BatchResult{}, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 5 {
		return BatchResult{}, ErrInvalid
	}
	for _, key := range []string{"workflow_id", "revision", "remaining", "batch_id"} {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return BatchResult{}, ErrInvalid
		}
	}
	if _, ok := fields["child"]; !ok {
		return BatchResult{}, ErrInvalid
	}
	if result.Child != nil && (result.Child.RunID == uuid.Nil || result.Child.State != "queued") {
		return BatchResult{}, ErrInvalid
	}
	return result, nil
}
