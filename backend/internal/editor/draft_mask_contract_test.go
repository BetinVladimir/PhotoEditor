package editor

import (
 "encoding/json"
 "testing"
)

func TestDraftMasksRequireArray(t *testing.T) {
 for _,raw:=range []string{`null`,`{}`,`true`,`"mask"`,`1`,``} {if validDraftMasks(json.RawMessage(raw))==nil {t.Errorf("accepted non-array %q",raw)}}
 for _,raw:=range []string{`[]`,`[{"artifact":{"artifact_id":"proof"}}]`} {if err:=validDraftMasks(json.RawMessage(raw));err!=nil {t.Errorf("array shape rejected %s: %v",raw,err)}}
}
