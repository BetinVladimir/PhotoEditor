package workflow

import (
	"encoding/json"
	"photoeditor/backend/internal/taskrecovery"
)

type Recovery = taskrecovery.Recovery

func ReduceExecutionProfile(p map[string]json.RawMessage) ([]byte, bool) {
	return taskrecovery.ReduceExecutionProfile(p)
}
func CPUFallbackProfile(p map[string]json.RawMessage) ([]byte, bool) {
	return taskrecovery.CPUFallbackProfile(p)
}
func cloneProfile(p map[string]json.RawMessage) map[string]json.RawMessage {
	q := make(map[string]json.RawMessage, len(p))
	for k, v := range p {
		q[k] = v
	}
	return q
}
