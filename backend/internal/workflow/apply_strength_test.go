package workflow

import (
	"encoding/json"
	"math"
	"testing"
)

func TestApplyResolveStrengthPrecedence(t *testing.T) {
	strength := 0.35
	for _, tc := range []struct { name, raw string; want float64 }{
		{"request", "", 0.35},
		{"job", `{"schema_version":1,"modules":{"strength":0.8,"local":{"enabled":false}}}`, 0.8},
		{"null inherits", `{"schema_version":1,"modules":{"strength":null}}`, 0.35},
		{"zero explicit", `{"schema_version":1,"modules":{"strength":0}}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyResolveJobOverride(&strength, json.RawMessage(tc.raw))
			if err != nil { t.Fatal(err) }
			var body struct { Schema int `json:"schema_version"`; Modules map[string]json.RawMessage `json:"modules"` }
			if err := json.Unmarshal(got, &body); err != nil { t.Fatal(err) }
			var value float64
			if err := json.Unmarshal(body.Modules["strength"], &value); err != nil { t.Fatal(err) }
			if value != tc.want || body.Schema != 1 { t.Fatalf("got %s", got) }
			if tc.name == "job" && string(body.Modules["local"]) != `{"enabled":false}` { t.Fatal("lost local override") }
		})
	}
}

func TestApplyResolveStrengthRejectsInvalidAndPreservesLegacy(t *testing.T) {
	for _, value := range []float64{-0.1, 1.1, math.NaN(), math.Inf(1)} {
		if _, err := applyResolveJobOverride(&value, nil); err == nil { t.Fatal("invalid strength accepted") }
	}
	raw := json.RawMessage(`{"schema_version":1,"modules":{"strength":0.7}}`)
	got, err := applyResolveJobOverride(nil, raw)
	if err != nil || string(got) != string(raw) { t.Fatal("legacy config changed") }
}
