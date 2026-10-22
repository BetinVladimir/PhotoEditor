package collection

import (
 "bytes"
 "encoding/json"
 "io"
 "photoeditor/backend/internal/domain"
)

// Position is required, even though zero is a valid first-section position.
// Decode before the receipt lock without altering the historical struct hash.
func (request *SectionCreate) UnmarshalJSON(raw []byte) error {
 invalid:=func()error{return domain.NewError("VALIDATION_FAILED",422)}
 decoder:=json.NewDecoder(bytes.NewReader(raw))
 token,err:=decoder.Token();if err!=nil||token!=json.Delim('{'){return invalid()}
 fields:=map[string]json.RawMessage{}
 for decoder.More(){
  token,err=decoder.Token();if err!=nil{return invalid()};key,ok:=token.(string);if !ok{return invalid()}
  if _,exists:=fields[key];exists{return invalid()};var value json.RawMessage
  if decoder.Decode(&value)!=nil{return invalid()};fields[key]=value
 }
 token,err=decoder.Token();if err!=nil||token!=json.Delim('}')||decoder.Decode(new(any))!=io.EOF||len(fields)!=3{return invalid()}
 for _,key:=range []string{"expected_revision","name","position"}{value,ok:=fields[key];if !ok||bytes.Equal(bytes.TrimSpace(value),[]byte("null")){return invalid()}}
 type wire SectionCreate
 var decoded wire
 exact:=json.NewDecoder(bytes.NewReader(raw));exact.DisallowUnknownFields()
 if exact.Decode(&decoded)!=nil||decoded.ExpectedRevision<1||!validSection(decoded.Name,decoded.Position){return invalid()}
 *request=SectionCreate(decoded);return nil
}
