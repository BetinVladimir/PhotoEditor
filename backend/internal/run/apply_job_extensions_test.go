package run

import (
	"encoding/json"
	"testing"
)

func TestApplyJobExtensionsShape(t *testing.T) {
	for _, raw := range []string{`{}`, `{"series":{"enabled":false}}`, `{"restoration":null,"region_adjustments":[]}`} {
		if !validApplyJobExtensions(json.RawMessage(raw)) { t.Fatalf("valid shape rejected: %s", raw) }
	}
	for _, raw := range []string{`null`,`[]`,`{"model_path":"unsafe"}`,`{"generation":{}}`,`{"series":{"enabled":null}}`,`{"series":{"enabled":false,"algorithm":"seriesmatch_v1"}}`,`{"restoration":false}`,`{"region_adjustments":null}`,`{"region_adjustments":[null]}`} {
		if validApplyJobExtensions(json.RawMessage(raw)) { t.Fatalf("invalid shape accepted: %s", raw) }
	}
}

func TestApplyRestorationCompleteContract(t *testing.T) {
	for _, raw := range []string{
		`{"denoise":"off","denoise_strength":0,"upscale":"off","sensor_dust":"off"}`,
		`{"denoise":"scunet_color_real_psnr","denoise_strength":1,"upscale":"4x","sensor_dust":"review"}`,
	} {
		if !validApplyJobExtensions(json.RawMessage(`{"restoration":`+raw+`}`)) { t.Fatalf("valid restoration rejected: %s",raw) }
	}
	for _, raw := range []string{
		`{"denoise":"off","upscale":"2x"}`,
		`{"denoise":"off","denoise_strength":null,"upscale":"off","sensor_dust":"off"}`,
		`{"denoise":"off","denoise_strength":1.1,"upscale":"off","sensor_dust":"off"}`,
		`{"denoise":"unknown","denoise_strength":0,"upscale":"off","sensor_dust":"off"}`,
		`{"denoise":"off","denoise_strength":0,"upscale":"8x","sensor_dust":"off"}`,
		`{"denoise":"off","denoise_strength":0,"upscale":"off","sensor_dust":"off","model_path":"x"}`,
	} {
		if validApplyJobExtensions(json.RawMessage(`{"restoration":`+raw+`}`)) { t.Fatalf("invalid restoration accepted: %s",raw) }
	}
}
