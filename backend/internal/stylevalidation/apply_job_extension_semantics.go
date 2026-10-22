package stylevalidation

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Extension mirrors contracts/style.py module semantics. Nullable
// extension slots and cross-module model dependencies are checked by the caller.
// Modules replace named modules, so required fields cannot be inherited here.
func Extension(name string, raw json.RawMessage) bool {
	if name == "region_adjustments" {
		var regions []json.RawMessage
		if json.Unmarshal(raw, &regions) != nil || regions == nil || len(regions) > 128 { return false }
		for _, region := range regions {
			f := applyExtObject(region)
			if !applyExtFields(f, "region_id selector ev saturation hue_deg strength priority", "") ||
				!applyExtString(f["region_id"], -1) || !applyExtEnum(f["selector"], "person", "background", "skin", "sky", "clothing", "manual") ||
				!applyExtNumber(f["ev"], -1, 1) || !applyExtNumber(f["saturation"], .7, 1.3) || !applyExtNumber(f["hue_deg"], -15, 15) ||
				!applyExtNumber(f["strength"], 0, 1) || !applyExtInt(f["priority"], -9007199254740991, 9007199254740991) { return false }
		}
		return true
	}
	f := applyExtObject(raw)
	if f == nil { return false }
	switch name {
	case "composition", "series", "culling", "portrait_detail", "inpainting", "generation":
		if applyExtLiteral(f["enabled"], "false") { return len(f) == 1 }
		if !applyExtLiteral(f["enabled"], "true") { return false }
	}
	switch name {
	case "composition":
		if !applyExtFields(f, "enabled aspect headroom rotate_limit_deg", "outpaint_policy outpaint_prompt outpaint_negative_prompt outpaint_seed outpaint_guidance_scale") ||
			!applyExtIntPair(f["aspect"], 1, 9007199254740991) || !applyExtNumber(f["headroom"], 0, 1) || !applyExtNumber(f["rotate_limit_deg"], 0, 5) { return false }
		policy := "off"
		if v, ok := f["outpaint_policy"]; ok { if !applyExtEnum(v, "off", "review") { return false }; _ = json.Unmarshal(v, &policy) }
		prompt, negative := "", ""
		for key, target := range map[string]*string{"outpaint_prompt": &prompt, "outpaint_negative_prompt": &negative} {
			if v, ok := f[key]; ok { if !applyExtString(v, 1000) { return false }; _ = json.Unmarshal(v, target) }
		}
		if v, ok := f["outpaint_seed"]; ok && !applyExtInt(v, 0, 4294967295) { return false }
		if v, ok := f["outpaint_guidance_scale"]; ok && !applyExtNumber(v, 1, 20) { return false }
		return policy == "review" && prompt != "" || policy == "off" && prompt == "" && negative == ""
	case "series":
		if !applyExtFields(f, "enabled algorithm reference ev_limit rgb_gain_limits", "") || !applyExtEnum(f["algorithm"], "seriesmatch_v1") || !applyExtString(f["reference"], -1) || !applyExtNumber(f["ev_limit"], 0, .35) { return false }
		v := applyExtPair(f["rgb_gain_limits"])
		return v != nil && applyExtNumber(v[0], .95, 1) && applyExtNumber(v[1], 1, 1.05)
	case "culling":
		if !applyExtFields(f, "enabled algorithm never_delete eye_policy", "target_count target_fraction") || !applyExtEnum(f["algorithm"], "cullrank_v1") || !applyExtLiteral(f["never_delete"], "true") || !applyExtEnum(f["eye_policy"], "review") { return false }
		count, fraction := f["target_count"], f["target_fraction"]
		hasCount, hasFraction := len(count) > 0 && !applyExtLiteral(count, "null"), len(fraction) > 0 && !applyExtLiteral(fraction, "null")
		return !(hasCount && hasFraction) && (!hasCount || applyExtInt(count, 1, 9007199254740991)) && (!hasFraction || applyExtNumber(fraction, math.SmallestNonzeroFloat64, 1))
	case "portrait_detail":
		return applyExtFields(f, "enabled teeth_strength shine_strength under_eye_strength glasses_policy", "") && applyExtNumber(f["teeth_strength"], 0, 1) && applyExtNumber(f["shine_strength"], 0, 1) && applyExtNumber(f["under_eye_strength"], 0, 1) && applyExtEnum(f["glasses_policy"], "review")
	case "inpainting":
		return applyExtFields(f, "enabled model policy mask_source", "") && applyExtEnum(f["model"], "big_lama") && applyExtEnum(f["policy"], "review") && applyExtEnum(f["mask_source"], "per_image")
	case "generation":
		return applyExtFields(f, "enabled model prompt negative_prompt seed steps guidance_scale strength canvas require_acceptance", "") &&
			applyExtEnum(f["model"], "sd15_inpaint") && applyExtString(f["prompt"], 2000) && applyExtString(f["negative_prompt"], 2000) &&
			applyExtInt(f["seed"], 0, 4294967295) && applyExtInt(f["steps"], 1, 50) && applyExtNumber(f["guidance_scale"], 0, 20) &&
			applyExtNumber(f["strength"], 0, 1) && applyExtIntPair(f["canvas"], 512, 512) && applyExtLiteral(f["require_acceptance"], "true")
	case "masking":
		return applyExtFields(f, "scene face interactive", "") && applyExtEnum(f["scene"], "segformer_b0_ade") && applyExtEnum(f["face"], "bisenet_face19") && applyExtEnum(f["interactive"], "mobilesam")
	case "background":
		if !applyExtFields(f, "mode asset_id blur_strength protect_subject", "solid_rgb") || !applyExtEnum(f["mode"], "keep", "transparent", "solid", "asset", "blur", "generate") || !applyExtNumber(f["blur_strength"], 0, 1) || !applyExtLiteral(f["protect_subject"], "true") { return false }
		asset := ""
		if !applyExtLiteral(f["asset_id"], "null") { if !applyExtString(f["asset_id"], -1) { return false }; _ = json.Unmarshal(f["asset_id"], &asset) }
		rgb, hasRGB := f["solid_rgb"]
		hasRGB = hasRGB && !applyExtLiteral(rgb, "null")
		if hasRGB {
			var channels []json.RawMessage
			if json.Unmarshal(rgb, &channels) != nil || len(channels) != 3 { return false }
			for _, channel := range channels { if !applyExtNumber(channel, 0, 1) { return false } }
		}
		return (!applyExtEnum(f["mode"], "asset") || asset != "") && (!applyExtEnum(f["mode"], "solid") || hasRGB)
	default:
		return false
	}
}

func applyExtObject(raw json.RawMessage) map[string]json.RawMessage {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') { return nil }
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok { return nil }
		if _, duplicate := fields[name]; duplicate { return nil }
		var value json.RawMessage
		if decoder.Decode(&value) != nil { return nil }
		fields[name] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') { return nil }
	var trailing any
	if decoder.Decode(&trailing) != io.EOF { return nil }
	return fields
}
func applyExtFields(fields map[string]json.RawMessage, required, optional string) bool {
	if fields == nil { return false }
	allowed := map[string]bool{}
	for _, name := range strings.Fields(required) { if _, ok := fields[name]; !ok { return false }; allowed[name] = true }
	for _, name := range strings.Fields(optional) { allowed[name] = true }
	for name := range fields { if !allowed[name] { return false } }
	return true
}
func applyExtLiteral(raw json.RawMessage, literal string) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte(literal)) }
func applyExtEnum(raw json.RawMessage, choices ...string) bool {
	var value string
	if !applyExtString(raw, -1) || json.Unmarshal(raw, &value) != nil { return false }
	for _, choice := range choices { if value == choice { return true } }
	return false
}
func applyExtString(raw json.RawMessage, limit int) bool {
	var value string
	return len(raw) > 0 && !applyExtLiteral(raw, "null") && json.Unmarshal(raw, &value) == nil && (limit < 0 || utf8.RuneCountInString(value) <= limit)
}
func applyExtNumber(raw json.RawMessage, low, high float64) bool {
	var value float64
	return len(raw) > 0 && !applyExtLiteral(raw, "null") && json.Unmarshal(raw, &value) == nil && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= low && value <= high
}
func applyExtInt(raw json.RawMessage, low, high int64) bool {
	value, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	return err == nil && value >= low && value <= high
}
func applyExtPair(raw json.RawMessage) []json.RawMessage {
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) != 2 { return nil }
	return values
}
func applyExtIntPair(raw json.RawMessage, low, high int64) bool {
	values := applyExtPair(raw)
	return values != nil && applyExtInt(values[0], low, high) && applyExtInt(values[1], low, high)
}
