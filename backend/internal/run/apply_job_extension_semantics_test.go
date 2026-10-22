package run

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApplyJobExtensionPortableSemantics(t *testing.T) {
	valid := map[string]string{
		"composition": `{"enabled":true,"aspect":[3,2],"headroom":0.1,"rotate_limit_deg":5}`,
		"series": `{"enabled":true,"algorithm":"seriesmatch_v1","reference":"auto","ev_limit":0.35,"rgb_gain_limits":[0.95,1.05]}`,
		"culling": `{"enabled":true,"algorithm":"cullrank_v1","never_delete":true,"eye_policy":"review"}`,
		"portrait_detail": `{"enabled":true,"teeth_strength":0.5,"shine_strength":0,"under_eye_strength":1,"glasses_policy":"review"}`,
		"inpainting": `{"enabled":true,"model":"big_lama","policy":"review","mask_source":"per_image"}`,
		"generation": `{"enabled":true,"model":"sd15_inpaint","prompt":"","negative_prompt":"","seed":4294967295,"steps":50,"guidance_scale":0,"strength":1,"canvas":[512,512],"require_acceptance":true}`,
		"masking": `{"scene":"segformer_b0_ade","face":"bisenet_face19","interactive":"mobilesam"}`,
		"background": `{"mode":"solid","asset_id":null,"solid_rgb":[0,0.5,1],"blur_strength":0,"protect_subject":true}`,
		"region_adjustments": `[{"region_id":"sky-1","selector":"sky","ev":-1,"saturation":0.7,"hue_deg":15,"strength":0.5,"priority":-2}]`,
	}
	for name, body := range valid {
		t.Run(name, func(t *testing.T) {
			if !validApplyJobExtension(name, json.RawMessage(body)) { t.Fatal("valid portable module rejected") }
			if name == "region_adjustments" { return }
			var fields map[string]json.RawMessage
			_ = json.Unmarshal([]byte(body), &fields)
			for field := range fields {
				// Optional defaults and nullable values are separately covered.
				if field == "solid_rgb" { continue }
				copyFields := make(map[string]json.RawMessage, len(fields))
				for key, value := range fields { copyFields[key] = value }
				delete(copyFields, field)
				raw, _ := json.Marshal(copyFields)
				if validApplyJobExtension(name, raw) { t.Errorf("missing required %s accepted", field) }
			}
			fields["unknown"] = json.RawMessage(`true`)
			raw, _ := json.Marshal(fields)
			if validApplyJobExtension(name, raw) { t.Fatal("unknown field accepted") }
		})
	}
	for _, name := range []string{"composition", "series", "culling", "portrait_detail", "inpainting", "generation"} {
		if !validApplyJobExtension(name, json.RawMessage(`{"enabled":false}`)) { t.Fatal("disabled module rejected", name) }
		if validApplyJobExtension(name, json.RawMessage(`{"enabled":false,"model":"big_lama"}`)) { t.Fatal("disabled extra fields accepted", name) }
	}
	for _, name := range []string{"unknown", "restoration", "composition"} {
		if validApplyJobExtension(name, json.RawMessage(`null`)) { t.Fatal("unhandled/null module accepted", name) }
	}
}

func TestApplyJobExtensionCrossChecksAndStrictTypes(t *testing.T) {
	cases := []struct { name, body string; valid bool }{
		{"composition", `{"enabled":true,"aspect":[3,2],"headroom":0,"rotate_limit_deg":0,"outpaint_policy":"review","outpaint_prompt":"expand"}`, true},
		{"composition", `{"enabled":true,"aspect":[3,2],"headroom":1,"rotate_limit_deg":0,"outpaint_policy":"review"}`, false},
		{"composition", `{"enabled":true,"aspect":[3,2],"headroom":0,"rotate_limit_deg":0,"outpaint_prompt":"expand"}`, false},
		{"composition", `{"enabled":true,"aspect":[3.0,2],"headroom":0,"rotate_limit_deg":0}`, false},
		{"composition", `{"enabled":true,"aspect":[3,0],"headroom":0,"rotate_limit_deg":0}`, false},
		{"composition", `{"enabled":true,"aspect":[3,2],"headroom":null,"rotate_limit_deg":0}`, false},
		{"composition", `{"enabled":true,"aspect":[3,2],"headroom":0,"rotate_limit_deg":0,"outpaint_guidance_scale":null}`, false},
		{"culling", `{"enabled":true,"algorithm":"cullrank_v1","never_delete":true,"eye_policy":"review","target_count":null,"target_fraction":null}`, true},
		{"culling", `{"enabled":true,"algorithm":"cullrank_v1","never_delete":true,"eye_policy":"review","target_count":1,"target_fraction":0.5}`, false},
		{"culling", `{"enabled":true,"algorithm":"cullrank_v1","never_delete":true,"eye_policy":"review","target_fraction":0}`, false},
		{"culling", `{"enabled":true,"algorithm":"cullrank_v1","never_delete":true,"eye_policy":"review","target_count":true}`, false},
		{"series", `{"enabled":true,"algorithm":"seriesmatch_v1","reference":"","ev_limit":0,"rgb_gain_limits":[1.01,1.05]}`, false},
		{"background", `{"mode":"asset","asset_id":"","blur_strength":0,"protect_subject":true}`, false},
		{"background", `{"mode":"asset","asset_id":"portable-key","blur_strength":1,"protect_subject":true}`, true},
		{"background", `{"mode":"solid","asset_id":null,"blur_strength":0,"protect_subject":true}`, false},
		{"background", `{"mode":"solid","asset_id":null,"solid_rgb":[0,0,1.1],"blur_strength":0,"protect_subject":true}`, false},
		{"background", `{"mode":"keep","asset_id":null,"blur_strength":0,"protect_subject":true}`, true},
		{"region_adjustments", `[]`, true},
		{"region_adjustments", `[{}]`, false},
		{"masking", `{"scene":"segformer_b0_ade","face":"bisenet_face19","interactive":null}`, false},
		{"inpainting", `{"enabled":false,"enabled":false}`, false},
		{"inpainting", `{"enabled":false} {}`, false},
	}
	for _, c := range cases {
		if got := validApplyJobExtension(c.name, json.RawMessage(c.body)); got != c.valid { t.Errorf("%s %s: got %v", c.name, c.body, got) }
	}
	// Limits count Unicode code points, as the Python string contract does.
	for _, count := range []int{1000, 1001} {
		raw, _ := json.Marshal(map[string]any{"enabled":true,"aspect":[]int{3,2},"headroom":0,"rotate_limit_deg":0,"outpaint_policy":"review","outpaint_prompt":strings.Repeat("я", count)})
		if got := validApplyJobExtension("composition", raw); got != (count == 1000) { t.Fatal("unicode prompt limit", count) }
	}
}

func TestApplyJobExtensionNumericAndEnumRejections(t *testing.T) {
	cases := []struct { name, body, field string; invalid []string }{
		{"composition", `{"enabled":true,"aspect":[3,2],"headroom":0.1,"rotate_limit_deg":1}`, "outpaint_seed", []string{`-1`,`4294967296`,`1.0`,`"1"`,`null`}},
		{"composition", `{"enabled":true,"aspect":[3,2],"headroom":0.1,"rotate_limit_deg":1}`, "outpaint_guidance_scale", []string{`0`,`20.1`,`"7.5"`,`null`}},
		{"generation", `{"enabled":true,"model":"sd15_inpaint","prompt":"","negative_prompt":"","seed":0,"steps":1,"guidance_scale":7.5,"strength":1,"canvas":[512,512],"require_acceptance":true}`, "steps", []string{`0`,`51`,`1.0`,`"1"`,`true`,`null`}},
		{"generation", `{"enabled":true,"model":"sd15_inpaint","prompt":"","negative_prompt":"","seed":0,"steps":1,"guidance_scale":7.5,"strength":1,"canvas":[512,512],"require_acceptance":true}`, "canvas", []string{`[512,513]`,`[512.0,512]`,`[512]`,`null`}},
		{"portrait_detail", `{"enabled":true,"teeth_strength":0.5,"shine_strength":0,"under_eye_strength":1,"glasses_policy":"review"}`, "teeth_strength", []string{`-0.1`,`1.1`,`true`,`"0.5"`,`null`}},
		{"inpainting", `{"enabled":true,"model":"big_lama","policy":"review","mask_source":"per_image"}`, "model", []string{`"sd15_inpaint"`,`null`,`1`}},
		{"series", `{"enabled":true,"algorithm":"seriesmatch_v1","reference":"auto","ev_limit":0.1,"rgb_gain_limits":[0.95,1.05]}`, "rgb_gain_limits", []string{`[0.94,1.05]`,`[0.95,1.06]`,`[1,0.99]`,`[1]`,`null`}},
	}
	for _, c := range cases {
		for _, bad := range c.invalid {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal([]byte(c.body), &fields)
			fields[c.field] = json.RawMessage(bad)
			raw, _ := json.Marshal(fields)
			if validApplyJobExtension(c.name, raw) { t.Errorf("%s.%s accepted %s", c.name, c.field, bad) }
		}
	}
	region := `{"region_id":"","selector":"manual","ev":0,"saturation":1,"hue_deg":0,"strength":0,"priority":0}`
	for _, count := range []int{128,129} {
		raw := json.RawMessage("["+strings.TrimSuffix(strings.Repeat(region+",", count), ",")+"]")
		if got := validApplyJobExtension("region_adjustments", raw); got != (count == 128) { t.Fatal("region count", count) }
	}
}
