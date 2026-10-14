package rawdevelopment

import (
 "bytes"
 "encoding/json"
 "errors"
 "photoeditor/backend/internal/canonical"
)

// UnmarshalJSON requires the full policy shape, with no null/defaulted fields.
func (p *Policy) UnmarshalJSON(raw []byte) error {
 type plain Policy
 var decoded plain
 normalized,err:=canonical.Bytes(raw)
 if err!=nil{return errors.New("invalid RAW development policy")}
 decoder:=json.NewDecoder(bytes.NewReader(normalized));decoder.DisallowUnknownFields()
 if decoder.Decode(&decoded)!=nil{return errors.New("invalid RAW development policy")}
 roundtrip,err:=canonical.Marshal(decoded)
 if err!=nil || !bytes.Equal(normalized,roundtrip) || !Valid((*Policy)(&decoded)) {
  return errors.New("invalid RAW development policy")
 }
 *p=Policy(decoded)
 return nil
}
