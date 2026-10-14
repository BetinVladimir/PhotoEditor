package provenance

import (
 "bytes"
 "encoding/json"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2optical"
)

// DecodeSigningNegativeQualification reads retained execution identity. It is
// not a live execution grant or proof that qualification reports were verified.
// Params must already be read physically through the scoped document reader.
func DecodeSigningNegativeQualification(profileRaw, paramsRaw []byte) (v2optical.NegativeQualificationPin,error) {
 var zero v2optical.NegativeQualificationPin
 params,err:=v2optical.ParseNegativeParams(paramsRaw)
 if err!=nil||len(profileRaw)<2||len(profileRaw)>1<<20 {return zero,ErrInvalid}
 normalized,err:=canonical.Bytes(profileRaw)
 if err!=nil {return zero,ErrInvalid}
 var profile map[string]json.RawMessage
 if json.Unmarshal(normalized,&profile)!=nil||profile==nil {return zero,ErrInvalid}
 pinRaw,exists:=profile["negative_qualification"]
 if !exists {return zero,ErrInvalid}
 var pin v2optical.NegativeQualificationPin
 parser:=json.NewDecoder(bytes.NewReader(pinRaw));parser.DisallowUnknownFields()
 if parser.Decode(&pin)!=nil {return zero,ErrInvalid}
 compiled,err:=pin.Compile()
 normalizedPin,normalizeErr:=canonical.Bytes(pinRaw)
 if err!=nil||normalizeErr!=nil||!bytes.Equal(compiled,normalizedPin)||pin.Calibration!=params.Calibration {return zero,ErrInvalid}
 for key,expected:=range map[string]string{"backend":"cpu","renderer_version":"v2.2","negative_runtime_sha":pin.RuntimeLockSHA} {
  var actual string
  if json.Unmarshal(profile[key],&actual)!=nil||actual!=expected {return zero,ErrInvalid}
 }
 return pin,nil
}
