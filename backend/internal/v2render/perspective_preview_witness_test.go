package v2render

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPerspectivePreviewAdmissionWitnessBindsOriginalObjects(t *testing.T) {
	f, bundle := previewArrayBundle(t, 2, .5)
	verified, err := VerifyPerspectivePreviewInputs(context.Background(), f, f, f, bundle)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := verified.AdmissionJSON()
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]json.RawMessage
	if err = json.Unmarshal(raw, &rows); err != nil || len(rows) != 2 {
		t.Fatal("missing typed witnesses", err)
	}
	var role, key string
	if err = json.Unmarshal(rows[0]["contract"], &role); err != nil || role != "WorkingPixels" {
		t.Fatal("wrong role", err)
	}
	if err = json.Unmarshal(rows[1]["object_key"], &key); err != nil || key != verified.Arrays()[1].Object.Key {
		t.Fatal("wrong locator", err)
	}
	raw[0] = 'x'
	again, err := verified.AdmissionJSON()
	if err != nil || again[0] == 'x' {
		t.Fatal("borrowed wire", err)
	}
	if _, err := (VerifiedPerspectivePreviewInputs{}).AdmissionJSON(); err == nil {
		t.Fatal("missing verification accepted")
	}
}
