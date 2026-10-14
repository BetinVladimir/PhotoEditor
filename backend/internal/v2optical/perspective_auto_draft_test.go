package v2optical

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestPerspectiveDraftLineOriginalOwnershipAndManualWire(t *testing.T) {
	manual := PerspectiveDraftSnapshot{ParamsJCS: "{}", SourceJCS: "{}"}
	raw, err := canonical.Marshal(manual)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("bad snapshot")
	}
	if _, ok := fields["lines_jcs"]; ok {
		t.Fatal("manual snapshot format changed")
	}
	if manual.lineOriginals() != nil {
		t.Fatal("manual unexpectedly has line inputs")
	}
	auto := manual
	auto.LinesJCS = `{"schema_version":1}`
	originals := auto.lineOriginals()
	originals[0][0] = '!'
	if auto.LinesJCS != `{"schema_version":1}` {
		t.Fatal("original aliases snapshot")
	}
	raw, err = canonical.Marshal(auto)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := (PerspectiveDraft{raw: string(raw)}).Snapshot()
	if err != nil || owned.LinesJCS != auto.LinesJCS {
		t.Fatal(owned, err)
	}
}
