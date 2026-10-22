package v2input

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuthoredNativeReplayIndexBudgetBoundaries(t *testing.T) {
	const native = `{"backend":"mps","model_lock_id":"native-lock","precision":"float32","resource_estimate":{"output_bytes":9,"pixel_count":1,"ram_bytes":2147483648,"scratch_bytes":4194322,"vram_bytes":1073741824},"tile_size":256}`
	for _, count := range []int{1, 32768, 32769, MaxEntries} {
		raw, err := AuthoredNativeReplayTransportProfile(native, count, strings.Repeat("a", 64))
		if err != nil {
			t.Fatalf("count %d: %v", count, err)
		}
		var p struct {
			Index    int64 `json:"input_index_scratch_bytes"`
			Resource struct {
				Scratch int64 `json:"scratch_bytes"`
				RAM     int64 `json:"ram_bytes"`
				VRAM    int64 `json:"vram_bytes"`
			} `json:"resource_estimate"`
		}
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		expected := int64(count) * 512
		if expected < 16<<20 {
			expected = 16 << 20
		}
		if p.Index != expected || p.Resource.Scratch != 4194322+expected || p.Resource.RAM != 2147483648 || p.Resource.VRAM != 1073741824 {
			t.Fatal("native/transport budget drift")
		}
	}
	for _, count := range []int{-1, 0, MaxEntries + 1} {
		if _, err := AuthoredNativeReplayTransportProfile(native, count, strings.Repeat("a", 64)); err == nil {
			t.Fatal("invalid count admitted")
		}
	}
	for _, changed := range []string{
		strings.Replace(native, `"scratch_bytes":4194322`, `"scratch_bytes":9007199254740991`, 1),
		strings.Replace(native, `"scratch_bytes":4194322`, `"scratch_bytes":1.5`, 1),
		strings.Replace(native, `"backend":"mps"`, `"backend":"cpu"`, 1),
		strings.Replace(native, `"tile_size":256`, `"input_index_scratch_bytes":1,"tile_size":256`, 1),
	} {
		if _, err := AuthoredNativeReplayTransportProfile(changed, 1, strings.Repeat("a", 64)); err == nil {
			t.Fatal("invalid native profile admitted")
		}
	}
}
