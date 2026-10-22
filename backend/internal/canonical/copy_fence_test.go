package canonical

import (
 "testing"
)

func TestCopyFenceLosslessCanonicalWire(t *testing.T) {
 for _, value := range []int64{1,9007199254740991,9007199254740992,9223372036854775807} {
  wire, err := CopyFenceWire(value)
  if err != nil { t.Fatal(err) }
  raw, err := Marshal(wire)
  if err != nil { t.Fatal(err) }
  restored, err := ParseCopyFenceWire(raw)
  if err != nil || restored != value { t.Fatal("fence changed", value, string(raw), err) }
 }
}

func TestCopyFenceRejectsAmbiguousOrUnsafeWire(t *testing.T) {
 for _, raw := range []string{`0`,`-1`,`true`,`null`,`1.0`,`1e0`,` 1`,`1 `,`01`,
  `9007199254740992`,`"1"`,`"01"`,`"9007199254740991"`,`"9223372036854775808"`,
  `"+9007199254740992"`,`"9e18"`,`"\u0039007199254740992"`,`"９００７１９９２５４７４０９９２"`} {
  if _, err := ParseCopyFenceWire([]byte(raw)); err == nil { t.Fatal("accepted invalid wire", raw) }
 }
 for _, value := range []int64{0,-1} { if _, err := CopyFenceWire(value); err == nil { t.Fatal("invalid fence accepted") } }
}

func TestCopyFenceDocumentProjectionPreservesFullInt64(t *testing.T) {
 type document struct { PreviewFence int64 `json:"preview_fence"`; Name string `json:"name"` }
 for _, value := range []int64{1,9007199254740991,9007199254740992,9223372036854775807} {
  original := document{PreviewFence:value,Name:"copy"}
  raw, err := MarshalCopyFenceDocument(original)
  if err != nil { t.Fatal(err) }
  if _, err := Bytes(raw); err != nil { t.Fatal("not JCS representable",err) }
  var restored document
  if err := UnmarshalCopyFenceDocument(raw,&restored); err != nil || restored != original { t.Fatal("document changed",err) }
 }
 var restored document
 for _, raw := range []string{`{"preview_fence":"9223372036854775808","name":"copy"}`,
  `{"preview_fence":9007199254740992,"name":"copy"}`,
  `{"preview_fence":1,"name":"copy","confirmed":true}`,
  `{"name":"copy"}`} {
  if err := UnmarshalCopyFenceDocument([]byte(raw),&restored); err == nil { t.Fatal("unsafe document accepted",raw) }
 }
}
