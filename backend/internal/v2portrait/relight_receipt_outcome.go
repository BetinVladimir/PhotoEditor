package v2portrait

import (
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// Only a verified absence allows the caller to enter fresh admission. Refusal,
// malformed receipt and changed request must not become an absent-receipt retry.
func decodeRelightReceiptOutcome(raw []byte) ([]byte,error) {
 if len(raw)<2 || len(raw)>131072 {return nil,v2input.ErrInvalid}
 normal,err:=canonical.Bytes(raw);if err!=nil {return nil,v2input.ErrInvalid}
 var fields map[string]json.RawMessage
 if json.Unmarshal(normal,&fields)!=nil {return nil,v2input.ErrInvalid}
 var outcome string
 if json.Unmarshal(fields["outcome"],&outcome)!=nil {return nil,v2input.ErrInvalid}
 if outcome=="receipt" {
  if len(fields)!=2 || len(fields["receipt"])<2 || fields["receipt"][0]!='{' {return nil,v2input.ErrInvalid}
  body,err:=canonical.Bytes(fields["receipt"]);if err!=nil || len(body)>65536 {return nil,v2input.ErrInvalid};return append([]byte(nil),body...),nil
 }
 if len(fields)!=1 {return nil,v2input.ErrInvalid}
 switch outcome {
 case "absent":return nil,v2input.ErrExpired
 case "denied":return nil,domain.NewError("NOT_FOUND",404)
 case "conflict":return nil,v2input.ErrConflict
 default:return nil,v2input.ErrInvalid
 }
}
