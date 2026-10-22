package v2input

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSceneInputIndexScratchBudget(t *testing.T) {
	pins := SemanticAnalysisRuntimePins{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)}
	for _, backend := range []string{"cuda", "mps"} {
		for _, op := range []string{"scene.parse", "depth.predict"} {
			for _, alpha := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", backend, op, alpha), func(t *testing.T) {
					costs := SemanticQualifiedCosts{RAMBytes: 17, VRAMBytes: 31, ScratchBytes: 43}
					before := costs
					compile := func(c SemanticQualifiedCosts, limit int64) (json.RawMessage, error) {
						return CompileSemanticAnalysisProfileForBackend(backend, op, 8, 8, alpha, 1024, pins, c, 1<<50, 1<<50, limit, 1<<50)
					}
					raw, err := compile(costs, 1<<50)
					if err != nil {
						t.Fatal(err)
					}
					var profile sessionDescribeExecutionProfile
					if json.Unmarshal(raw, &profile) != nil {
						t.Fatal("decode")
					}
					per := int64(2)
					if op == "depth.predict" {
						per = 4
						if alpha {
							per += 4
						}
					}
					expected := per*64 + 16384 + (1 << 20) + 1024 + 43 + (16 << 20)
					if profile.ResourceEstimate.ScratchBytes != expected {
						t.Fatalf("scratch %d expected %d", profile.ResourceEstimate.ScratchBytes, expected)
					}
					if _, err = compile(costs, expected); err != nil {
						t.Fatal(err)
					}
					if _, err = compile(costs, expected-1); err == nil {
						t.Fatal("under capacity accepted")
					}
					if costs != before {
						t.Fatal("caller costs mutated")
					}
					costs.ScratchBytes = 9007199254740991 - expected + 43
					if _, err = compile(costs, 9007199254740991); err != nil {
						t.Fatal("exact safe boundary", err)
					}
					costs.ScratchBytes++
					if _, err = compile(costs, 9007199254740991); err == nil {
						t.Fatal("overflow accepted")
					}
				})
			}
		}
	}
}
