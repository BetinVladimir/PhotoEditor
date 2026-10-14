package rawdevelopment

import "photoeditor/backend/internal/v2qualification"

// These checks cover every advertised setting, not just default AHD/camera WB.
var requiredChecks = []string{
 "raw.camera_matrix", "raw.demosaic.ahd", "raw.demosaic.dcb",
 "raw.wb.camera", "raw.wb.daylight", "raw.wb.auto",
 "raw.highlights.clip", "raw.highlights.blend", "raw.highlights.reconstruct",
 "raw.exposure.bounds", "raw.icc.roundtrip", "raw.originals.unchanged",
}

// Capability is constructed only from independently trusted qualification pins.
// Report bytes alone cannot grant an advertised decoding capability.
type Capability struct { decoderVersion string }

type CapabilityView struct {
 DecoderVersion string `json:"decoder_version"`
 Demosaic []string `json:"demosaic"`
 WBSource []string `json:"wb_source"`
 HighlightPolicy []string `json:"highlight_policy"`
 ExposureMin float64 `json:"exposure_min"`
 ExposureMax float64 `json:"exposure_max"`
}

func VerifyCapability(raw []byte,reportSHA,decoderVersion string,expected v2qualification.Expected) (*Capability,error) {
 probe:=&Policy{SchemaVersion:1,Demosaic:"ahd",WBSource:"camera",HighlightPolicy:"clip",DecoderVersion:decoderVersion}
 if !Valid(probe) || expected.Operation!="asset.decode" || expected.Version!="1" || expected.Backend!="cpu" {
  return nil,v2qualification.ErrUnqualified
 }
 // Add mandatory RAW checks to a copy; caller-owned pin sets remain unchanged.
 checks:=append([]string(nil),expected.RequiredChecks...)
 seen:=map[string]bool{}
 for _,check:=range checks {seen[check]=true}
 for _,check:=range requiredChecks {if !seen[check] {checks=append(checks,check);seen[check]=true}}
 expected.RequiredChecks=checks
 if _,err:=v2qualification.Verify(raw,reportSHA,expected);err!=nil{return nil,err}
 return &Capability{decoderVersion:decoderVersion},nil
}

func (c *Capability) Allows(policy *Policy) bool {
 return c!=nil && c.decoderVersion!="" && policy!=nil && Valid(policy) && policy.DecoderVersion==c.decoderVersion
}

func (c *Capability) View() (CapabilityView,bool) {
 if c==nil || c.decoderVersion=="" {return CapabilityView{},false}
 return CapabilityView{DecoderVersion:c.decoderVersion,Demosaic:[]string{"ahd","dcb"},
  WBSource:[]string{"camera","daylight","auto"},HighlightPolicy:[]string{"clip","blend","reconstruct"},
  ExposureMin:-2,ExposureMax:3},true
}
