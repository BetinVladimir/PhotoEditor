package workflow

import (
	"encoding/json"
	"testing"
)

func profile(t *testing.T, s string) map[string]json.RawMessage {
	t.Helper()
	var p map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestProfileReductionRequiresDeclaredBounds(t *testing.T) {
	p := profile(t, `{"backend":"mps","batch_size":4,"min_batch_size":1,"tile_size":512,"min_tile_size":128,"model_lock":"unchanged"}`)
	b, ok := ReduceExecutionProfile(p)
	if !ok {
		t.Fatal("expected batch reduction")
	}
	q := profile(t, string(b))
	if string(q["batch_size"]) != "2" || string(q["tile_size"]) != "512" || string(q["model_lock"]) != `"unchanged"` {
		t.Fatal(string(b))
	}
	if string(p["batch_size"]) != "4" {
		t.Fatal("mutated source profile")
	}
	if _, ok = ReduceExecutionProfile(profile(t, `{"tile_size":512}`)); ok {
		t.Fatal("undeclared tile reduction")
	}
	b, ok = ReduceExecutionProfile(profile(t, `{"tile_size":200,"min_tile_size":128}`))
	if !ok || string(profile(t, string(b))["tile_size"]) != "128" {
		t.Fatal("minimum tile not respected")
	}
}
func TestCPUFallbackRequiresPermissionAndQualification(t *testing.T) {
	for _, s := range []string{`{"backend":"mps","allow_cpu_fallback":true}`, `{"backend":"cpu","allow_cpu_fallback":true,"cpu_qualified":true}`, `{"backend":"cuda","cpu_qualified":true}`} {
		if _, ok := CPUFallbackProfile(profile(t, s)); ok {
			t.Fatal(s)
		}
	}
	b, ok := CPUFallbackProfile(profile(t, `{"backend":"cuda","precision":"fp16","allow_cpu_fallback":true,"cpu_qualified":true,"model_lock":"same","cpu_resource_estimate":{"ram_bytes":8192,"vram_bytes":0,"scratch_bytes":1024,"output_bytes":0,"pixel_count":100}}`))
	if !ok {
		t.Fatal("expected explicit fallback")
	}
	q := profile(t, string(b))
	if string(q["backend"]) != `"cpu"` || string(q["precision"]) != `"fp32"` || string(q["model_lock"]) != `"same"` {
		t.Fatal(string(b))
	}
}

func TestResourceEstimateStrict(t *testing.T) {
	for _, raw := range []string{`{}`, `{"resource_estimate":{"ram_bytes":1}}`, `{"resource_estimate":{"ram_bytes":0,"vram_bytes":0,"scratch_bytes":0}}`, `{"resource_estimate":{"ram_bytes":1.5,"vram_bytes":0,"scratch_bytes":0}}`, `{"resource_estimate":{"ram_bytes":2,"vram_bytes":-1,"scratch_bytes":0}}`, `{"resource_estimate":{"ram_bytes":2,"vram_bytes":0,"scratch_bytes":0,"unknown":1}}`} {
		if _, err := ParseResourceEstimate([]byte(raw)); err == nil {
			t.Fatal("accepted invalid estimate", raw)
		}
	}
	e, err := ParseResourceEstimate([]byte(`{"backend":"cpu","resource_estimate":{"ram_bytes":2048,"vram_bytes":0,"scratch_bytes":1024,"output_bytes":0,"pixel_count":0}}`))
	if err != nil || e.RAMBytes != 2048 || e.ScratchBytes != 1024 {
		t.Fatal(e, err)
	}
}
