package v2input

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSemanticAnalysisQualifiedBackendCapacityAndIdentity(t *testing.T) {
	pins := SemanticAnalysisRuntimePins{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)}
	const limit int64 = 64 << 30
	for _, op := range []string{"scene.parse", "depth.predict"} {
		costs := SemanticQualifiedCosts{VRAMBytes: 1}
		legacy, err := CompileSemanticAnalysisProfile(op, 32, 32, false, 1024, pins, costs, limit, limit, limit, limit)
		if err != nil {
			t.Fatal(err)
		}
		cuda, err := CompileSemanticAnalysisProfileForBackend("cuda", op, 32, 32, false, 1024, pins, costs, limit, limit, limit, limit)
		if err != nil || !bytes.Equal(legacy, cuda) {
			t.Fatal("CUDA default changed", err)
		}
		costs.VRAMBytes = 11 << 30
		metal, err := CompileSemanticAnalysisProfileForBackend("mps", op, 32, 32, false, 1024, pins, costs, limit, limit, limit, limit)
		if err != nil {
			t.Fatal("measured Metal capacity incorrectly CUDA-capped", err)
		}
		var profile struct {
			Backend string `json:"backend"`
		}
		if json.Unmarshal(metal, &profile) != nil || profile.Backend != "mps" {
			t.Fatal("backend identity lost")
		}
		if _, err = CompileSemanticAnalysisProfileForBackend("mps", op, 32, 32, false, 1024, pins, costs, limit, costs.VRAMBytes-1, limit, limit); err == nil {
			t.Fatal("measured device capacity ignored")
		}
		if _, err = CompileSemanticAnalysisProfileForBackend("cuda", op, 32, 32, false, 1024, pins, costs, limit, limit, limit, limit); err == nil {
			t.Fatal("CUDA existing ceiling lost")
		}
		if _, err = CompileSemanticAnalysisProfileForBackend("cpu", op, 32, 32, false, 1024, pins, costs, limit, limit, limit, limit); err == nil {
			t.Fatal("unreviewed backend admitted")
		}
	}
}
