package semanticreview

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "os"
 "testing"
)

func TestRemovalPreviewSharedCanonicalWire(t *testing.T) {
 raw, err := os.ReadFile("testdata/removal_preview_params.json")
 if err != nil { t.Fatal(err) }
 p, err := ParseRemovalPreviewParams(raw)
 if err != nil { t.Fatal(err) }
 if p.WorkflowRevision != "9007199254740993" || p.TargetRevision != "9223372036854775806" { t.Fatal("revision precision lost") }
 frozen, digest, err := FreezeRemovalPreviewParams(p)
 sum := sha256.Sum256(raw)
 if err != nil || !bytes.Equal(raw, frozen) || digest != hex.EncodeToString(sum[:]) { t.Fatal("shared wire changed", err) }
 for _, bad := range []string{"0","01","9223372036854775807","90071992547409930x"} {
  changed := p; changed.WorkflowRevision = bad
  if _, _, err := FreezeRemovalPreviewParams(changed); err == nil { t.Fatal("invalid revision", bad) }
 }
 for _, pair := range [][2]int{{32,1},{512,64}} {
  changed := p; changed.ContextPixels = pair[0]; changed.FeatherPixels = pair[1]
  if _, _, err := FreezeRemovalPreviewParams(changed); err != nil { t.Fatal("valid boundary", pair, err) }
 }
 for _, pair := range [][2]int{{31,1},{513,1},{32,0},{32,65}} {
  changed := p; changed.ContextPixels = pair[0]; changed.FeatherPixels = pair[1]
  if _, _, err := FreezeRemovalPreviewParams(changed); err == nil { t.Fatal("invalid boundary", pair) }
 }
}

func TestShownRemovalSharedLosslessFenceWire(t *testing.T) {
 raw, err := os.ReadFile("testdata/removal_preview_adoption_params.json")
 if err != nil { t.Fatal(err) }
 p, err := ParseShownRemovalAdoptionParams(raw)
 if err != nil || p.PreviewFence != 9223372036854775807 { t.Fatal("int64 fence changed",err) }
 frozen, _, err := FreezeShownRemovalAdoptionParams(p)
 if err != nil || !bytes.Equal(raw,frozen) { t.Fatal("shared copy bytes changed",err) }
}
