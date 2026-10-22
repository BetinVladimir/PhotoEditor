package stylevalidation

import (
	"bytes"
	"encoding/json"
	"math"
)

// Validate complete named extension modules before capability lookup. Nullable
// slots clear inherited modules; region_adjustments is a non-null array.
func Extensions(raw json.RawMessage) bool {
 modules,ok := applyColorObject(raw,nil,[]string{"culling","series","masking","composition","portrait_detail","restoration","background","inpainting","generation","region_adjustments"})
 if !ok { return false }
 for name, body := range modules {
  if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
   if name == "region_adjustments" { return false }
   continue
  }
  if name == "restoration" {
   fields, valid := applyColorObject(body,[]string{"denoise","denoise_strength","upscale","sensor_dust"},nil)
   if !valid || !validApplyRestoration(fields) { return false }
  } else if !Extension(name,body) { return false }
 }
 return true
}

func validApplyRestoration(fields map[string]json.RawMessage) bool {
	if len(fields) != 4 { return false }
	choices := map[string][]string{
		"denoise":{"off","scunet_color_real_psnr"},
		"upscale":{"off","2x","4x"},
		"sensor_dust":{"off","review","conservative_auto"},
	}
	for name, allowed := range choices {
		var value string
		if json.Unmarshal(fields[name], &value) != nil { return false }
		found := false
		for _, choice := range allowed { found = found || value == choice }
		if !found { return false }
	}
	var strength float64
	raw, exists := fields["denoise_strength"]
	return exists && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && json.Unmarshal(raw, &strength)==nil && !math.IsNaN(strength) && !math.IsInf(strength,0) && strength>=0 && strength<=1
}
