package semanticproposal

import (
	"encoding/json"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDefectPermanentMatchesCompleteDeclaredMask(t *testing.T) {
	body, err := os.ReadFile("../review/testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Protection json.RawMessage `json:"protection"`
	}
	if json.Unmarshal(body, &report) != nil {
		t.Fatal("report fixture")
	}
	params, err := canonical.Marshal(map[string]any{"permanent": report.Protection})
	if err != nil {
		t.Fatal(err)
	}
	if err := matchDefectPermanent(params, report.Protection); err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if json.Unmarshal(report.Protection, &original) != nil {
		t.Fatal("mask fixture")
	}
	for _, test := range []struct {
		field string
		value any
	}{{"confidence", .25}, {"selector", "skin"}, {"protected", false}} {
		changed := map[string]any{}
		for key, value := range original {
			changed[key] = value
		}
		changed[test.field] = test.value
		altered, err := canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := matchDefectPermanent(params, altered); err != v2input.ErrInvalid {
			t.Fatal("changed mask accepted", test.field, err)
		}
	}
	for _, invalid := range [][]byte{[]byte(`{}`), []byte(`{"permanent":null}`), []byte(`{"permanent":{}}`), []byte(`{`)} {
		if err := matchDefectPermanent(invalid, report.Protection); err != v2input.ErrInvalid {
			t.Fatal("absent or invalid permanent accepted", err)
		}
	}
}
