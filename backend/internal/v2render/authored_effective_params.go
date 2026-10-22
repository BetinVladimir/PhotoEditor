package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// CompileAuthoredEffectiveParams derives the deterministic claim published in
// AuthoredLayerStyleBinding from immutable ResolvedStyle, never by changing its
// image or adaptive decision. It mirrors the installed pure effective algorithm;
// worker/adopter still independently run resolve_effective_params and compare.
// This compiler is content only, not original producer or current SQL authority.
func CompileAuthoredEffectiveParams(raw []byte) ([]byte, error) {
	fail := func() ([]byte, error) { return nil, v2input.ErrInvalid }
	if len(raw) < 2 || len(raw) > 1<<20 {
		return fail()
	}
	wire, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(wire, raw) {
		return fail()
	}
	var resolved map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&resolved) != nil {
		return fail()
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return fail()
	}
	saved, ok := resolved["hash"].(string)
	if !ok || !authoredFinalDigest(saved) {
		return fail()
	}
	delete(resolved, "hash")
	body, err := canonical.Marshal(resolved)
	if err != nil {
		return fail()
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != saved {
		return fail()
	}
	mode, ok := resolved["mode"].(string)
	if !ok || (mode != "fixed" && mode != "adaptive" && mode != "fixed_fallback" && mode != "group_fixed") {
		return fail()
	}
	number, ok := resolved["strength"].(json.Number)
	if !ok {
		return fail()
	}
	strength, err := number.Float64()
	if err != nil || math.IsNaN(strength) || math.IsInf(strength, 0) || strength < 0 || strength > 1 {
		return fail()
	}
	modules, ok := resolved["modules"].(map[string]any)
	if !ok {
		return fail()
	}
	scale := func(module map[string]any, fields ...string) error {
		for _, field := range fields {
			value, ok := module[field].(json.Number)
			if !ok {
				return v2input.ErrInvalid
			}
			n, err := value.Float64()
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return v2input.ErrInvalid
			}
			module[field] = n * strength
		}
		return nil
	}
	active := func(name string) (map[string]any, bool, error) {
		m, ok := modules[name].(map[string]any)
		if !ok {
			return nil, false, v2input.ErrInvalid
		}
		enabled, ok := m["enabled"].(bool)
		if !ok {
			return nil, false, v2input.ErrInvalid
		}
		return m, enabled, nil
	}
	for _, entry := range []struct {
		name   string
		fields []string
	}{{"beauty", []string{"smoothing", "tone_evenness"}}, {"defects", []string{"strength"}}, {"geometry", []string{"body_strength", "face_slim"}}} {
		m, enabled, err := active(entry.name)
		if err != nil {
			return fail()
		}
		if enabled {
			if err = scale(m, entry.fields...); err != nil {
				return fail()
			}
		}
	}
	extensions, ok := modules["extensions"].(map[string]any)
	if !ok {
		return fail()
	}
	for _, entry := range []struct {
		name    string
		fields  []string
		enabled bool
	}{{"portrait_detail", []string{"teeth_strength", "shine_strength", "under_eye_strength"}, true}, {"restoration", []string{"denoise_strength"}, false}, {"background", []string{"blur_strength"}, false}, {"generation", []string{"strength"}, true}} {
		value, present := extensions[entry.name]
		if !present || value == nil {
			continue
		}
		m, ok := value.(map[string]any)
		if !ok {
			return fail()
		}
		if entry.enabled {
			enabled, ok := m["enabled"].(bool)
			if !ok {
				return fail()
			}
			if !enabled {
				continue
			}
		}
		if err = scale(m, entry.fields...); err != nil {
			return fail()
		}
	}
	return canonical.Marshal(map[string]any{"version": "effective_params_v1", "resolved_hash": saved, "color_strength": strength, "modules": modules})
}
