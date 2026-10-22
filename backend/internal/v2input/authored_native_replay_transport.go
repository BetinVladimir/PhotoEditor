package v2input

import (
	"encoding/json"
	"strings"

	"photoeditor/backend/internal/canonical"
)

// AuthoredNativeReplayTransportProfile preserves the native computation budget
// and admits the separately live input-index allocation. entryCount is the
// count of the frozen complete closure, not the number of direct roots.
// Qualification and current deployment selection remain mandatory caller proofs.
func AuthoredNativeReplayTransportProfile(original string, entryCount int, contractsSHA string) (string, error) {
	if entryCount < 1 || entryCount > MaxEntries || !digestPattern.MatchString(contractsSHA) {
		return "", ErrInvalid
	}
	raw := []byte(original)
	canonicalRaw, err := canonical.Bytes(raw)
	if err != nil || string(canonicalRaw) != original {
		return "", ErrInvalid
	}
	var profile map[string]json.RawMessage
	if json.Unmarshal(raw, &profile) != nil || profile == nil {
		return "", ErrInvalid
	}
	var backend string
	if json.Unmarshal(profile["backend"], &backend) != nil || (backend != "cuda" && backend != "mps") {
		return "", ErrInvalid
	}
	if prior, exists := profile["renderer_version"]; exists && string(prior) != `"v2.2"` {
		return "", ErrInvalid
	}
	var resources map[string]json.RawMessage
	if json.Unmarshal(profile["resource_estimate"], &resources) != nil || resources == nil {
		return "", ErrInvalid
	}
	for _, key := range []string{"ram_bytes", "vram_bytes", "scratch_bytes", "output_bytes", "pixel_count"} {
		var value int64
		if json.Unmarshal(resources[key], &value) != nil || value < 0 || value > 9007199254740991 || ((key == "pixel_count" || key == "vram_bytes") && value == 0) {
			return "", ErrInvalid
		}
	}
	var scratch int64
	if json.Unmarshal(resources["scratch_bytes"], &scratch) != nil {
		return "", ErrInvalid
	}
	var previous int64
	if declared, exists := profile["input_index_scratch_bytes"]; exists {
		if json.Unmarshal(declared, &previous) != nil || previous < 0 || previous > scratch {
			return "", ErrInvalid
		}
		if previous > 0 {
			if string(profile["renderer_version"]) != `"v2.2"` {
				return "", ErrInvalid
			}
			var oldCount int
			if json.Unmarshal(profile["v2_entry_count"], &oldCount) != nil || oldCount < 1 || oldCount > MaxEntries {
				return "", ErrInvalid
			}
			expected := int64(oldCount) * 512
			if expected < 16<<20 {
				expected = 16 << 20
			}
			if previous != expected {
				return "", ErrInvalid
			}
		}
	}
	// Subtract only an explicitly declared, independently validated old index.
	scratch -= previous
	for _, key := range []string{"v2_input_manifest", "v2_entry_count", "v2_entries_sha"} {
		delete(profile, key)
	}
	overhead := int64(entryCount) * 512
	if overhead < 16<<20 {
		overhead = 16 << 20
	}
	if scratch > 9007199254740991-overhead {
		return "", ErrInvalid
	}
	encode := func(value any) json.RawMessage { result, _ := json.Marshal(value); return result }
	resources["scratch_bytes"] = encode(scratch + overhead)
	encodedResources, err := canonical.Marshal(resources)
	if err != nil {
		return "", err
	}
	profile["resource_estimate"] = encodedResources
	profile["renderer_version"] = encode("v2.2")
	profile["contracts_sha"] = encode(contractsSHA)
	profile["input_index_scratch_bytes"] = encode(overhead)
	result, err := canonical.Marshal(profile)
	if err != nil || strings.TrimSpace(string(result)) == "" {
		return "", ErrInvalid
	}
	return string(result), nil
}
