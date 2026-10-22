package styleplan

import (
	"encoding/json"
	"testing"
)

func TestAuthoredBasePlanPreservesProcessingAndDefersLayers(t *testing.T) {
	p, err := CompileAuthoredBasePlan(json.RawMessage(`{"mode":"fixed","geometry":{"enabled":true},"beauty":{"enabled":true},"defects":{"enabled":true,"policy":"review"},"extensions":{"portrait_detail":{"enabled":true}}}`), 2)
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]int{}
	for i, stage := range p.Stages {
		if stage.Phase == "editor_precompose" || stage.Operation == "editor.layers_precompose" {
			t.Fatal("authored layers would execute before the base")
		}
		positions[stage.Phase] = i
	}
	previous := -1
	for _, phase := range []string{"geometry", "mask_transform", "beauty", "portrait_detail", "defects_propose", "defects_review", "quality"} {
		position, ok := positions[phase]
		if !ok || position <= previous {
			t.Fatalf("processing stage missing or reordered: %s", phase)
		}
		previous = position
	}
}

func TestAuthoredBasePlanDigestBindsCanonicalStyle(t *testing.T) {
	a, err := CompileAuthoredBasePlan(json.RawMessage(`{"mode":"fixed","beauty":{"enabled":false}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CompileAuthoredBasePlan(json.RawMessage(`{ "beauty": { "enabled": false }, "mode": "fixed" }`), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.SHA256) != 64 || a.SHA256 != b.SHA256 || string(a.EffectiveStyle) != string(b.EffectiveStyle) {
		t.Fatal("equivalent styles must retain one immutable plan identity")
	}
	c, err := CompileAuthoredBasePlan(json.RawMessage(`{"mode":"fixed","beauty":{"enabled":true}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.SHA256 == c.SHA256 {
		t.Fatal("processing changes must invalidate the plan identity")
	}
}

func TestAuthoredBasePlanRejectsInvalidInputs(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{`), json.RawMessage(`{"mode":"unknown"}`)} {
		if _, err := CompileAuthoredBasePlan(raw, 1); err == nil {
			t.Fatal("invalid style accepted")
		}
	}
	for _, count := range []int{0, 5001} {
		if _, err := CompileAuthoredBasePlan(json.RawMessage(`{"mode":"fixed"}`), count); err == nil {
			t.Fatal("invalid item count accepted")
		}
	}
}
