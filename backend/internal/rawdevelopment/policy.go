package rawdevelopment

import (
 "math"
 "regexp"
)

// RAWDevelopment is frozen in decode Params; nil retains the legacy decoder.
type Policy struct {
 SchemaVersion int `json:"schema_version"`
 Demosaic string `json:"demosaic"`
 WBSource string `json:"wb_source"`
 ExposureEV float64 `json:"exposure_ev"`
 HighlightPolicy string `json:"highlight_policy"`
 DecoderVersion string `json:"decoder_version"`
}

var rawDecoderVersion = regexp.MustCompile(`^rawpy-[0-9]+\.[0-9]+\.[0-9]+/libraw-[0-9]+\.[0-9]+\.[0-9]+$`)

func Valid(p *Policy) bool {
 if p == nil { return true }
 return p.SchemaVersion == 1 && (p.Demosaic == "ahd" || p.Demosaic == "dcb") &&
  (p.WBSource == "camera" || p.WBSource == "daylight" || p.WBSource == "auto") &&
  !math.IsNaN(p.ExposureEV) && !math.IsInf(p.ExposureEV,0) && p.ExposureEV >= -2 && p.ExposureEV <= 3 &&
  (p.HighlightPolicy == "clip" || p.HighlightPolicy == "blend" || p.HighlightPolicy == "reconstruct") &&
  len(p.DecoderVersion) <= 128 && rawDecoderVersion.MatchString(p.DecoderVersion)
}
