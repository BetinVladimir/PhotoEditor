package canonical

import (
 "bytes"
 "io"
 "encoding/json"
 "errors"
 "strconv"
)

const MaxSafeCopyFence int64 = 9007199254740991

// CopyFenceWire keeps historic safe numbers unchanged and encodes larger
// positive int64 producer fences as decimal strings for lossless JCS transport.
func CopyFenceWire(value int64) (any, error) {
 if value < 1 { return nil, errors.New("invalid copy fence") }
 if value <= MaxSafeCopyFence { return value, nil }
 return strconv.FormatInt(value, 10), nil
}

// ParseCopyFenceWire accepts exactly one encoding per value. Small strings,
// unsafe numbers, whitespace, exponents and leading zeros are not canonical.
func ParseCopyFenceWire(raw []byte) (int64, error) {
 invalid := errors.New("invalid copy fence wire")
 if len(raw) == 0 || len(raw) > 21 { return 0, invalid }
 digits := string(raw)
 quoted := raw[0] == '"'
 if quoted {
  var text string
  if json.Unmarshal(raw, &text) != nil { return 0, invalid }
  digits = text
  // Escape sequences are never part of the canonical decimal representation.
  if string(raw) != "\""+text+"\"" { return 0, invalid }
 }
 if len(digits) == 0 || digits[0] < '1' || digits[0] > '9' { return 0, invalid }
 for _, digit := range digits { if digit < '0' || digit > '9' { return 0, invalid } }
 value, err := strconv.ParseInt(digits, 10, 64)
 if err != nil || value < 1 || quoted != (value > MaxSafeCopyFence) { return 0, invalid }
 return value, nil
}

// MarshalCopyFenceDocument projects an int64 field into lossless JSON. Pass a
// plain struct alias to avoid recursion from a document's MarshalJSON method.
func MarshalCopyFenceDocument(document any) ([]byte, error) {
 raw, err := json.Marshal(document)
 if err != nil { return nil, err }
 var fields map[string]json.RawMessage
 if err := json.Unmarshal(raw, &fields); err != nil { return nil, err }
 value, err := strconv.ParseInt(string(fields["preview_fence"]), 10, 64)
 if err != nil { return nil, err }
 wire, err := CopyFenceWire(value)
 if err != nil { return nil, err }
 encoded, err := json.Marshal(wire)
 if err != nil { return nil, err }
 fields["preview_fence"] = encoded
 return json.Marshal(fields)
}

// UnmarshalCopyFenceDocument restores a wire fence before strict struct
// decoding. The outer document parser still verifies exact canonical bytes.
func UnmarshalCopyFenceDocument(raw []byte, target any) error {
 if len(raw)<2 || len(raw)>1048576 { return errors.New("invalid copy document size") }
 var fields map[string]json.RawMessage
 if err := json.Unmarshal(raw, &fields); err != nil { return err }
 value, err := ParseCopyFenceWire(fields["preview_fence"])
 if err != nil { return err }
 fields["preview_fence"] = json.RawMessage(strconv.FormatInt(value,10))
 decoded, err := json.Marshal(fields)
 if err != nil { return err }
 decoder := json.NewDecoder(bytes.NewReader(decoded))
 decoder.DisallowUnknownFields()
 if err := decoder.Decode(target); err != nil { return err }
 var extra any
 if decoder.Decode(&extra) != io.EOF { return errors.New("trailing copy document") }
 return nil
}
